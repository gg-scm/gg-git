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
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"gg-scm.io/pkg/git/object"
)

// DeltaReader decompresses a deltified object from a packfile.
// See details at https://git-scm.com/docs/pack-format#_deltified_representation
type DeltaReader struct {
	base  io.ReadSeeker
	delta ByteReader

	inited       bool
	initError    error
	expandedSize int64

	curr      io.Reader
	remaining uint32
}

// NewDeltaReader returns a new DeltaReader that applies the given delta to a
// base object.
func NewDeltaReader(base io.ReadSeeker, delta ByteReader) *DeltaReader {
	return &DeltaReader{
		base:  base,
		delta: delta,
	}
}

func (d *DeltaReader) init() error {
	if d.inited {
		return d.initError
	}
	d.inited = true
	_, expandedSize, err := readDeltaHeader(d.delta)
	if err != nil {
		d.initError = fmt.Errorf("apply delta: %w", d.initError)
		return d.initError
	}
	if expandedSize >= 1<<63 {
		d.initError = fmt.Errorf("apply delta: expanded size (%d) too large", expandedSize)
		return d.initError
	}
	d.expandedSize = int64(expandedSize)
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

// Size returns the expected size of the decompressed bytes as reported by the
// delta header. Use DeltaObjectSize to determine the precise number of bytes
// that the DeltaReader will produce.
func (d *DeltaReader) Size() (int64, error) {
	if err := d.init(); err != nil {
		return 0, err
	}
	return d.expandedSize, nil
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
		if _, err := d.base.Seek(int64(offset), io.SeekStart); err != nil {
			return fmt.Errorf("apply delta: %w", err)
		}
		d.curr = d.base
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
		if err == io.EOF {
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
			if err := copyN(io.Discard, delta, int64(instruction)); err != nil {
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
		if err == io.EOF {
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
		if err == io.EOF {
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

// ResolveType determines the type of the object located at the given offset.
// If the object is a deltified, then it follows the delta base object
// references until it encounters a non-delta object and returns its type.
func ResolveType(f ByteReadSeeker, offset int64, opts *UndeltifyOptions) (object.Type, error) {
	hdr, _, err := walkDeltaChain(f, offset, opts)
	if err != nil {
		return "", fmt.Errorf("packfile: resolve type at %d: %w", offset, err)
	}
	return hdr.Type.NonDelta(), nil
}

// An Undeltifier decompresses deltified objects in a packfile. The zero value
// is a valid Undeltifier. Undeltifiers have cached internal state, so
// Undeltifiers should be reused instead of created as needed.
//
// For more information on deltification, see
// https://git-scm.com/docs/pack-format#_deltified_representation
type Undeltifier struct {
	z  zlibReader
	zr *bufio.Reader

	baseBuf    *bytes.Buffer
	baseReader bytes.Reader
	targetBuf  *bytes.Buffer
}

// UndeltifyOptions contains optional parameters for processing deltified
// objects.
type UndeltifyOptions struct {
	// Index allows the undeltify operation to resolve delta object base ID
	// references within the same packfile.
	Index *Index
}

// Undeltify decompresses the object at the given offset from the beginning of
// the packfile, undeltifying the object if needed. The returned io.Reader
// may read from f, so the caller should not use f until they are done reading
// from the returned io.Reader.
func (u *Undeltifier) Undeltify(f ByteReadSeeker, offset int64, opts *UndeltifyOptions) (object.Prefix, io.Reader, error) {
	hdr, deltaBodyStack, err := walkDeltaChain(f, offset, opts)
	if err != nil {
		return object.Prefix{}, nil, fmt.Errorf("packfile: %w", err)
	}

	// We've found the root of the delta chain. Read it into memory.
	if err := setZlibReader(&u.z, f); err != nil {
		return object.Prefix{}, nil, fmt.Errorf("packfile: undeltify %v at %d: read base at %d: %w", hdr.Type, offset, hdr.Offset, err)
	}
	typ := hdr.Type.NonDelta()
	if len(deltaBodyStack) == 0 {
		// The originally requested object was not deltified. As an optimization,
		// skip copying it into memory and return the stream directly.
		return object.Prefix{Type: typ, Size: hdr.Size}, u.z, nil
	}
	if hdr.Size > maxDeltaObjectSize {
		return object.Prefix{}, nil, fmt.Errorf("packfile: undeltify %v at %d: read base at %d: object too large (%d bytes)", hdr.Type, offset, hdr.Offset, hdr.Size)
	}
	if u.baseBuf == nil {
		u.baseBuf = bytes.NewBuffer(make([]byte, 0, int(hdr.Size)))
	} else {
		u.baseBuf.Reset()
		u.baseBuf.Grow(int(hdr.Size))
	}
	if _, err := io.Copy(u.baseBuf, u.z); err != nil {
		return object.Prefix{}, nil, fmt.Errorf("packfile: undeltify %v at %d: read base at %d: %w", hdr.Type, offset, hdr.Offset, err)
	}
	for len(deltaBodyStack) > 0 {
		deltaBodyStart := deltaBodyStack[len(deltaBodyStack)-1]
		deltaBodyStack = deltaBodyStack[:len(deltaBodyStack)-1]
		if _, err := f.Seek(deltaBodyStart, io.SeekStart); err != nil {
			return object.Prefix{}, nil, fmt.Errorf("packfile: undeltify %v at %d: %w", hdr.Type, offset, err)
		}
		if err := u.undeltify(typ, f); err != nil {
			return object.Prefix{}, nil, fmt.Errorf("packfile: undeltify %v at %d: %w", hdr.Type, offset, err)
		}
		u.baseBuf, u.targetBuf = u.targetBuf, u.baseBuf
	}
	u.baseReader.Reset(u.baseBuf.Bytes())
	return object.Prefix{Type: typ, Size: u.baseReader.Size()}, &u.baseReader, nil
}

// walkDeltaChain follows the delta base object references until it encounters
// a non-delta object. On success, it returns the non-delta header and the
// positions of the delta zlib-compressed payloads in reverse order, and f's
// read position will be at the start of the zlib-compressed data of the
// base object.
func walkDeltaChain(f ByteReadSeeker, offset int64, opts *UndeltifyOptions) (*Header, []int64, error) {
	if opts == nil {
		opts = new(UndeltifyOptions)
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, nil, fmt.Errorf("undeltify object at %d: %w", offset, err)
	}
	brc := &byteReaderCounter{r: f}
	hdr, err := readObjectHeader(offset, brc)
	if err != nil {
		return nil, nil, fmt.Errorf("undeltify object at %d: %w", offset, err)
	}
	var deltaBodyStack []int64
	for hdr.Type.NonDelta() == "" {
		deltaBodyStack = append(deltaBodyStack, hdr.Offset+brc.n)
		baseOffset := hdr.BaseOffset
		if hdr.Type == RefDelta {
			i := opts.Index.FindID(hdr.BaseObject)
			if i == -1 {
				return nil, nil, fmt.Errorf("undeltify object at %d: could not find %v in index", hdr.Offset, hdr.BaseObject)
			}
			baseOffset = opts.Index.Offsets[i]
		}
		if _, err := f.Seek(baseOffset, io.SeekStart); err != nil {
			return nil, nil, fmt.Errorf("undeltify object at %d: %w", hdr.Offset, err)
		}
		brc.n = 0
		hdr, err = readObjectHeader(baseOffset, brc)
		if err != nil {
			return nil, nil, fmt.Errorf("undeltify object at %d: %w", hdr.Offset, err)
		}
	}
	return hdr, deltaBodyStack, nil
}

// undeltify runs the zlib-compressed delta instructions in compressStream on
// u.baseBuf and writes to u.targetBuf.
func (u *Undeltifier) undeltify(typ object.Type, compressStream ByteReader) error {
	if err := setZlibReader(&u.z, compressStream); err != nil {
		return err
	}
	defer setZlibReader(&u.z, emptyReader{}) // don't retain deltaPackObject past function return
	u.baseReader.Reset(u.baseBuf.Bytes())
	if u.zr == nil {
		u.zr = bufio.NewReader(u.z)
	} else {
		u.zr.Reset(u.z)
	}
	newObjectReader := NewDeltaReader(&u.baseReader, u.zr)
	newSize, err := newObjectReader.Size()
	if err != nil {
		return err
	}
	if u.targetBuf == nil {
		u.targetBuf = bytes.NewBuffer(make([]byte, 0, int(newSize)))
	} else {
		u.targetBuf.Reset()
		u.targetBuf.Grow(int(newSize))
	}
	if _, err := io.Copy(u.targetBuf, newObjectReader); err != nil {
		return err
	}
	return nil
}

// ByteReadSeeker is the interface that groups the io.Reader, io.ByteReader,
// and io.Seeker interfaces.
type ByteReadSeeker interface {
	io.Reader
	io.ByteReader
	io.Seeker
}

// BufferedReadSeeker implements buffering for an io.ReadSeeker object.
type BufferedReadSeeker struct {
	r io.ReadSeeker
	b *bufio.Reader
}

// NewBufferedReadSeeker returns a new BufferedReadSeeker whose buffer has the
// default size.
func NewBufferedReadSeeker(r io.ReadSeeker) *BufferedReadSeeker {
	return NewBufferedReadSeekerSize(r, 4096)
}

// NewBufferedReadSeekerSize returns a new BufferedReadSeeker whose buffer has
// at least the specified size.
func NewBufferedReadSeekerSize(r io.ReadSeeker, size int) *BufferedReadSeeker {
	return &BufferedReadSeeker{
		r: r,
		b: bufio.NewReaderSize(r, size),
	}
}

// Read reads data into p. The bytes are taken from at most one Read on the
// underlying Reader, hence n may be less than len(p). To read exactly len(p)
// bytes, use io.ReadFull(b, p). At EOF, the count will be zero and err will be
// io.EOF.
func (rs *BufferedReadSeeker) Read(p []byte) (int, error) {
	return rs.b.Read(p)
}

// ReadByte reads and returns a single byte. If no byte is available, returns an
// error.
func (rs *BufferedReadSeeker) ReadByte() (byte, error) {
	return rs.b.ReadByte()
}

// Seek implements the io.Seeker interface.
func (rs *BufferedReadSeeker) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekCurrent {
		if 0 <= offset && offset <= int64(rs.b.Buffered()) {
			// Optimization: if we move forward by less bytes than buffered, just
			// discard bytes from the buffer.
			currPos, err := rs.r.Seek(0, io.SeekCurrent)
			if err != nil {
				return 0, err
			}
			rs.b.Discard(int(offset))
			return currPos - int64(rs.b.Buffered()), nil
		}
		// We've already read rs.b.Buffered() bytes beyond what we're surfacing to
		// the caller, so we need to correct the offset before sending it to the
		// underlying Seek.
		offset -= int64(rs.b.Buffered())
	}
	newPos, err := rs.r.Seek(offset, whence)
	if err != nil {
		return 0, err
	}
	rs.b.Reset(rs.r)
	return newPos, nil
}
