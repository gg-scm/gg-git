// Copyright 2020 The gg Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package packfile

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
)

// DeltaReader decompresses a deltified object from a packfile.
// See details at https://git-scm.com/docs/pack-format#_deltified_representation
type DeltaReader struct {
	base  io.ReaderAt
	delta ByteReader

	inited    bool
	curr      io.Reader
	remaining uint32
}

// NewDeltaReader returns a new DeltaReader that applies the given delta to a
// base object.
func NewDeltaReader(base io.ReaderAt, delta ByteReader) *DeltaReader {
	return &DeltaReader{
		base:  base,
		delta: delta,
	}
}

func (d *DeltaReader) init() error {
	if d.inited {
		return nil
	}
	if _, _, err := readDeltaHeader(d.delta); err != nil {
		return fmt.Errorf("apply delta: %w", err)
	}
	d.inited = true
	return nil
}

func readDeltaHeader(r io.ByteReader) (baseObjectSize, expandedSize uint64, err error) {
	baseObjectSize, err = binary.ReadUvarint(r)
	if err != nil {
		return
	}
	expandedSize, err = binary.ReadUvarint(r)
	if err != nil {
		return
	}
	return
}

// Read implements io.Reader by decompressing the deltified object.
func (d *DeltaReader) Read(p []byte) (int, error) {
	if d.curr == nil {
		if err := d.init(); err != nil {
			return 0, err
		}
		if err := d.readInstruction(); err != nil {
			return 0, err
		}
	}
	// Now we know where we're reading from. Read until we get to end of length.
	if int64(len(p)) > int64(d.remaining) {
		p = p[:int(d.remaining)]
	}
	n, err := d.curr.Read(p)
	d.remaining -= uint32(n)
	if d.remaining == 0 {
		err = nil
		d.curr = nil
	}
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}

func (d *DeltaReader) readInstruction() error {
	instruction, err := d.delta.ReadByte()
	if err == io.EOF {
		return io.EOF
	}
	if err != nil {
		return fmt.Errorf("apply delta: %w", err)
	}
	switch {
	case instruction&0x80 != 0:
		// Copy from base
		offset, size, err := readCopyBaseInstruction(instruction, d.delta)
		if err != nil {
			return fmt.Errorf("apply delta: %w", err)
		}
		d.curr = io.NewSectionReader(d.base, int64(offset), int64(size))
		d.remaining = size
	case instruction != 0:
		// Add new data
		d.curr = d.delta
		d.remaining = uint32(instruction)
	default:
		return fmt.Errorf("apply delta: unknown instruction")
	}
	return nil
}

// DeltaObjectSize calculates the size of an object constructed from delta
// instructions.
func DeltaObjectSize(delta ByteReader) (int64, error) {
	if _, _, err := readDeltaHeader(delta); err != nil {
		return 0, fmt.Errorf("calculate delta object size: %w", err)
	}
	var n int64
	for {
		instruction, err := delta.ReadByte()
		if errors.Is(err, io.EOF) {
			return n, nil
		}
		if err != nil {
			return 0, fmt.Errorf("calculate delta object size: %w", err)
		}
		switch {
		case instruction&0x80 != 0:
			// Copy from base
			_, nn, err := readCopyBaseInstruction(instruction, delta)
			if err != nil {
				return 0, fmt.Errorf("calculate delta object size: %w", err)
			}
			n += int64(nn)
		case instruction != 0:
			// Add new data
			n += int64(instruction)
			if err := copyN(ioutil.Discard, delta, int64(instruction)); err != nil {
				return 0, fmt.Errorf("calculate delta object size: %w", err)
			}
		default:
			return 0, fmt.Errorf("calculate delta object size: unknown instruction")
		}
	}
}

// readCopyBaseInstruction parses an instruction to copy from base object.
// https://git-scm.com/docs/pack-format#_instruction_to_copy_from_base_object
func readCopyBaseInstruction(instruction byte, r io.ByteReader) (offset, size uint32, _ error) {
	for i, shift := 0, 0; i < 4; i, shift = i+1, shift+8 {
		if instruction&(1<<i) == 0 {
			continue
		}
		b, err := r.ReadByte()
		if errors.Is(err, io.EOF) {
			return 0, 0, io.ErrUnexpectedEOF
		}
		if err != nil {
			return 0, 0, err
		}
		offset |= uint32(b) << shift
	}
	for i, shift := 4, 0; i < 7; i, shift = i+1, shift+8 {
		if instruction&(1<<i) == 0 {
			continue
		}
		b, err := r.ReadByte()
		if errors.Is(err, io.EOF) {
			return 0, 0, io.ErrUnexpectedEOF
		}
		if err != nil {
			return 0, 0, err
		}
		size |= uint32(b) << shift
	}
	if size == 0 {
		size = 0x10000
	}
	return
}

// copyN is a copy of io.CopyN but returns ErrUnexpectedEOF if less than n bytes
// where copied.
func copyN(dst io.Writer, src io.Reader, n int64) error {
	written, err := io.Copy(dst, io.LimitReader(src, n))
	if written == n {
		return nil
	}
	if written < n && err == nil {
		// src stopped early; must have been EOF.
		err = io.ErrUnexpectedEOF
	}
	return err
}
