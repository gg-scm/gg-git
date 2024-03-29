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
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"gg-scm.io/pkg/git/githash"
	"gg-scm.io/pkg/git/object"
)

// ByteReader is a combination of io.Reader and io.ByteReader.
type ByteReader interface {
	io.Reader
	io.ByteReader
}

// Reader reads a packfile serially.
//
// Use ReadHeader if you want random access in a packfile.
type Reader struct {
	r     byteReaderCounter
	nobjs uint32

	dataReader zlibReader
	remaining  int64
}

// NewReader returns a Reader that reads from the given stream.
func NewReader(r ByteReader) *Reader {
	return &Reader{r: byteReaderCounter{r: r}}
}

func (r *Reader) init() error {
	if r.r.n > 0 {
		return nil
	}
	var err error
	r.nobjs, err = readFileHeader(&r.r)
	return err
}

const fileHeaderSize = 12

func readFileHeader(r io.Reader) (uint32, error) {
	var buf [fileHeaderSize]byte
	if _, err := io.ReadFull(r, buf[:]); errors.Is(err, io.EOF) {
		return 0, fmt.Errorf("packfile: read header: %w", io.ErrUnexpectedEOF)
	} else if err != nil {
		return 0, fmt.Errorf("packfile: read header: %w", err)
	}
	if buf[0] != 'P' || buf[1] != 'A' || buf[2] != 'C' || buf[3] != 'K' {
		return 0, errors.New("packfile: read header: incorrect signature")
	}
	version := ntohl(buf[4:])
	if version != 2 {
		return 0, fmt.Errorf("packfile: read header: version is %d (only supports 2)", version)
	}
	nobjs := ntohl(buf[8:])
	return nobjs, nil
}

// Next advances to the next object in the packfile. The Header.Size determines
// how many bytes can be read for the next object. Any remaining data in the
// current object is automatically discarded.
//
// io.EOF is returned at the end of the input.
func (r *Reader) Next() (*Header, error) {
	if err := r.init(); err != nil {
		return nil, err
	}
	if r.dataReader != nil {
		n, err := io.Copy(io.Discard, r.dataReader)
		if err != nil {
			return nil, fmt.Errorf("packfile: advance to next object: %w", err)
		}
		if n > r.remaining {
			return nil, errTooLong
		}
		if n < r.remaining {
			return nil, errTooShort
		}
		r.dataReader.Close()
	}
	if r.nobjs == 0 {
		// Consume trailing checksum.
		// TODO(someday): Verify integrity. This is a SHA-1 hash.
		if _, err := io.CopyN(io.Discard, &r.r, githash.SHA1Size); err != nil {
			return nil, fmt.Errorf("packfile: read trailing checksum: %w", err)
		}
		return nil, io.EOF
	}
	hdr, err := readObjectHeader(r.r.n, &r.r)
	if err != nil {
		return nil, err
	}
	if err := setZlibReader(&r.dataReader, &r.r); err != nil {
		return nil, fmt.Errorf("packfile: %w", err)
	}
	r.remaining = hdr.Size
	r.nobjs--
	return hdr, nil
}

// Read reads from the current object in the packfile. It returns (0, io.EOF)
// when it reaches the end of that object, until Next is called to advance to
// the next object.
func (r *Reader) Read(p []byte) (int, error) {
	if r.dataReader == nil {
		return 0, fmt.Errorf("packfile: Read() called before Next()")
	}
	if r.remaining < 0 {
		return 0, errTooLong
	}
	n, err := r.dataReader.Read(p)
	r.remaining -= int64(n)
	if r.remaining < 0 {
		// We've gone past the declared size. Don't return more bytes than we
		// advertised to the caller.
		return n - int(-r.remaining), errTooLong
	}
	if errors.Is(err, io.EOF) && r.remaining > 0 {
		err = fmt.Errorf("packfile: %w", io.ErrUnexpectedEOF)
	} else if err != nil && !errors.Is(err, io.EOF) {
		err = fmt.Errorf("packfile: %w", err)
	}
	return n, err
}

var (
	errTooLong  = errors.New("packfile: object longer than header size")
	errTooShort = errors.New("packfile: object shorter than header size")
)

// A Header holds a single object header in a packfile.
type Header struct {
	// Offset is the location in the packfile this object starts at. It can be
	// used as a key for BaseOffset. Writer ignores this field.
	Offset int64

	Type ObjectType

	// Size is the uncompressed size of the object in bytes.
	Size int64

	// BaseOffset is the Offset of a previous Header for an OffsetDelta type object.
	BaseOffset int64
	// BaseObject is the hash of an object for a RefDelta type object.
	BaseObject githash.SHA1
}

// ReadHeader reads a packfile object header from r. The returned Header's
// Offset field will be set to the given offset. If ReadHeader does not return
// an error, the data of the object will be available on r as a zlib-compressed
// stream.
func ReadHeader(offset int64, r ByteReader) (*Header, error) {
	hdr, err := readObjectHeader(offset, r)
	if err != nil {
		return nil, fmt.Errorf("packfile: %w", err)
	}
	return hdr, nil
}

func readObjectHeader(offset int64, r ByteReader) (*Header, error) {
	hdr := &Header{Offset: offset}
	var err error
	hdr.Type, hdr.Size, err = readLengthType(r)
	if err != nil {
		return nil, err
	}
	switch hdr.Type {
	case OffsetDelta:
		off, err := readOffset(r)
		if err != nil {
			return nil, err
		}
		hdr.BaseOffset = hdr.Offset + off
	case RefDelta:
		if _, err := io.ReadFull(r, hdr.BaseObject[:]); err != nil {
			return nil, fmt.Errorf("read ref-delta object: %w", err)
		}
	}
	return hdr, nil
}

func readLengthType(br io.ByteReader) (ObjectType, int64, error) {
	first, err := br.ReadByte()
	if err != nil {
		return 0, 0, fmt.Errorf("read packfile object length+type: %w", err)
	}
	typ := ObjectType(first >> 4 & 7)
	if typ == 0 || typ == 5 {
		return 0, 0, fmt.Errorf("read packfile object length+type: invalid type %d", int(typ))
	}
	n := int64(first & 0xf)
	if first&0x80 != 0 {
		nn, err := binary.ReadUvarint(br)
		if err != nil {
			return typ, 0, fmt.Errorf("read packfile object length+type: %w", err)
		}
		if nn >= 1<<(63-4) {
			return typ, 0, fmt.Errorf("read packfile object length+type: too large")
		}
		n |= int64(nn << 4)
	}
	return typ, n, nil
}

// readOffset parses the offset encoding from
// https://git-scm.com/docs/pack-format.
//
// n bytes with MSB set in all but the last one.
// The offset is then the number constructed by
// concatenating the lower 7 bit of each byte, and
// for n >= 2 adding 2^7 + 2^14 + ... + 2^(7*(n-1))
// to the result.
func readOffset(br io.ByteReader) (int64, error) {
	var bits int64
	var accum int64
	for i := 0; i < 8; i++ {
		b, err := br.ReadByte()
		if err != nil {
			return 0, fmt.Errorf("read packfile base offset: %w", err)
		}
		bits <<= 7
		bits |= int64(b & 0x7f)
		if b&0x80 == 0 {
			return -(bits + accum), nil
		}
		accum += 1 << ((i + 1) * 7)
	}
	return 0, fmt.Errorf("read packfile base offset: too large")
}

// An ObjectType holds the type of an object inside a packfile.
type ObjectType int8

// Object types
const (
	Commit ObjectType = 1
	Tree   ObjectType = 2
	Blob   ObjectType = 3
	Tag    ObjectType = 4

	OffsetDelta ObjectType = 6
	RefDelta    ObjectType = 7
)

func (typ ObjectType) isValid() bool {
	return typ == Commit ||
		typ == Tree ||
		typ == Blob ||
		typ == Tag ||
		typ == OffsetDelta ||
		typ == RefDelta
}

// NonDelta returns the Git object type that the packfile object type represents
// or the empty string if the type represents a deltified object.
func (typ ObjectType) NonDelta() object.Type {
	switch typ {
	case Commit:
		return object.TypeCommit
	case Tree:
		return object.TypeTree
	case Blob:
		return object.TypeBlob
	case Tag:
		return object.TypeTag
	default:
		return ""
	}
}

// String returns the Git object type constant name like "OBJ_COMMIT".
func (t ObjectType) String() string {
	switch t {
	case Commit:
		return "OBJ_COMMIT"
	case Tree:
		return "OBJ_TREE"
	case Blob:
		return "OBJ_BLOB"
	case Tag:
		return "OBJ_TAG"
	case OffsetDelta:
		return "OBJ_OFS_DELTA"
	case RefDelta:
		return "OBJ_REF_DELTA"
	default:
		return fmt.Sprintf("ObjectType(%d)", int8(t))
	}
}

type byteReaderCounter struct {
	r ByteReader
	n int64
}

func (brc *byteReaderCounter) Read(p []byte) (int, error) {
	n, err := brc.r.Read(p)
	brc.n += int64(n)
	return n, err
}

func (brc *byteReaderCounter) ReadByte() (byte, error) {
	b, err := brc.r.ReadByte()
	if err != nil {
		return 0, err
	}
	brc.n++
	return b, err
}

type zlibReader interface {
	io.Reader
	io.Closer
	zlib.Resetter
}

func setZlibReader(z *zlibReader, r ByteReader) error {
	if *z == nil {
		zr, err := zlib.NewReader(r)
		if err != nil {
			return err
		}
		*z = zr.(zlibReader)
		return nil
	}
	return (*z).Reset(r, nil)
}
