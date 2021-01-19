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
	"compress/zlib"
	"crypto/sha1"
	"fmt"
	"hash"
	"io"
)

// Writer writes a packfile.
type Writer struct {
	wc    writerCounter
	nobjs uint32
	hash  hash.Hash

	// Scratch buffer
	buf []byte

	// Objects
	dataWriter    *zlib.Writer
	dataRemaining int64
}

// NewWriter returns a Writer that writes to the given stream. It is the
// caller's responsibility to call Close on the returned Writer after the last
// object has been written.
func NewWriter(w io.Writer, objectCount uint32) *Writer {
	h := sha1.New()
	return &Writer{
		wc:    writerCounter{w: io.MultiWriter(h, w)},
		nobjs: objectCount,
		hash:  h,
	}
}

func (w *Writer) init() error {
	if w.wc.n > 0 {
		return nil
	}
	fileHeader := []byte{
		'P', 'A', 'C', 'K',
		0, 0, 0, 2, // version 2
		0, 0, 0, 0,
	}
	htonl(fileHeader[8:], w.nobjs)
	if _, err := w.wc.Write(fileHeader); err != nil {
		return fmt.Errorf("packfile: write header: %w", err)
	}
	return nil
}

// WriteHeader writes hdr and prepares to accept the object's contents.
// WriteHeader returns the offset of the header from the beginning of the
// stream. The Header.Size determines how many bytes can be written for the next
// object. If the current object is not fully written or WriteHeader has been
// called more times than the object count passed to NewWriter, WriteHeader
// returns an error.
func (w *Writer) WriteHeader(hdr *Header) (offset int64, err error) {
	// Check preconditions.
	if !hdr.Type.isValid() {
		return 0, fmt.Errorf("packfile: write object header: invalid type %d", int(hdr.Type))
	}
	if hdr.BaseOffset < 0 {
		return 0, fmt.Errorf("packfile: write object header: invalid base offset %d", hdr.BaseOffset)
	}
	if w.dataRemaining > 0 {
		return 0, fmt.Errorf("packfile: write object header: previous object incomplete (%d bytes remaining)", w.dataRemaining)
	}

	// Write file header or close out previous object.
	if err := w.init(); err != nil {
		return 0, err
	}
	if w.dataWriter != nil {
		if err := w.dataWriter.Close(); err != nil {
			return 0, fmt.Errorf("packfile: write object: %w", err)
		}
	}

	// Write object header.
	if w.nobjs == 0 {
		return 0, fmt.Errorf("packfile: more objects written than declared")
	}
	w.nobjs--
	offset = w.wc.n
	w.buf = appendLengthType(w.buf[:0], hdr.Type, hdr.Size)
	switch hdr.Type {
	case OffsetDelta:
		w.buf = appendOffset(w.buf, hdr.BaseOffset-offset)
	case RefDelta:
		w.buf = append(w.buf, hdr.BaseObject[:]...)
	}
	if _, err := w.wc.Write(w.buf); err != nil {
		return offset, fmt.Errorf("packfile: write object: %w", err)
	}

	// Prepare object writer.
	if w.dataWriter == nil {
		w.dataWriter = zlib.NewWriter(&w.wc)
	} else {
		w.dataWriter.Reset(&w.wc)
	}
	w.dataRemaining = hdr.Size
	return offset, nil
}

// Write writes to the current object in the packfile. Write returns an error if
// more than the Header.Size bytes are written after WriteHeader.
func (w *Writer) Write(p []byte) (n int, err error) {
	if w.dataWriter == nil {
		return 0, fmt.Errorf("packfile: Write() called before WriteHeader()")
	}
	if len(p) == 0 {
		return 0, nil
	}
	tooLong := false
	if int64(len(p)) > w.dataRemaining {
		p = p[:int(w.dataRemaining)]
		tooLong = true
	}
	n, err = w.dataWriter.Write(p)
	w.dataRemaining -= int64(n)
	if err != nil {
		return n, fmt.Errorf("packfile: write object: %w", err)
	}
	if tooLong {
		return n, fmt.Errorf("packfile: write object: too long")
	}
	return n, nil
}

// Close closes the packfile by writing the trailer. If the current object
// (from a prior call to WriteHeader) is not fully written or WriteHeader has
// been called less times than the object count passed to NewWriter, Close
// returns an error. This method does not close the underlying writer.
func (w *Writer) Close() error {
	if w.nobjs > 0 {
		return fmt.Errorf("packfile: close: less objects written than declared (%d more expected)", w.nobjs)
	}
	if w.dataRemaining > 0 {
		return fmt.Errorf("packfile: close: previous object incomplete (%d bytes remaining)", w.dataRemaining)
	}
	if err := w.init(); err != nil {
		return err
	}
	if w.dataWriter != nil {
		if err := w.dataWriter.Close(); err != nil {
			return fmt.Errorf("packfile: close: %w", err)
		}
	}
	if _, err := w.wc.Write(w.hash.Sum(nil)); err != nil {
		return fmt.Errorf("packfile: close: write trailer: %w", err)
	}
	return nil
}

func appendLengthType(dst []byte, typ ObjectType, n int64) []byte {
	msb := byte(0)
	if n >= 0x10 {
		msb = 0x80
	}
	dst = append(dst, byte(typ)<<4|byte(n&0xf)|msb)
	if msb != 0 {
		dst = appendVarint(dst, uint64(n>>4))
	}
	return dst
}

func appendVarint(dst []byte, x uint64) []byte {
	for x >= 0x80 {
		dst = append(dst, byte(x)|0x80)
		x >>= 7
	}
	dst = append(dst, byte(x))
	return dst
}

func appendOffset(dst []byte, x int64) []byte {
	// All offsets are negative. Work in positive integer space.
	x = -x
	// Append little-endian quasi-varint.
	start := len(dst)
	dst = append(dst, byte(x&0x7f))
	for {
		x = x >> 7
		if x == 0 {
			break
		}
		x-- // The `- 1` makes it different from varint.
		dst = append(dst, 0x80|byte(x&0x7f))
	}
	// Reverse bytes for big-endian order.
	for i, j := start, len(dst)-1; i < j; i, j = i+1, j-1 {
		dst[i], dst[j] = dst[j], dst[i]
	}
	return dst
}

type writerCounter struct {
	w io.Writer
	n int64
}

func (wc *writerCounter) Write(p []byte) (int, error) {
	n, err := wc.w.Write(p)
	wc.n += int64(n)
	return n, err
}
