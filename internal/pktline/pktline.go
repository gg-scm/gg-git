// Copyright 2021 The gg Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//		 https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

// Package pktline reads and writes the pkt-line format described in
// https://git-scm.com/docs/protocol-common#_pkt_line_format.
package pktline

import (
	"encoding/hex"
	"fmt"
	"io"
)

// MaxSize is the maximum number of bytes permitted in a single pkt-line.
const MaxSize = 65516

// Type indicates the type of a packet.
type Type int8

// Packet types.
const (
	// Flush indicates the end of a message.
	Flush Type = 0
	// Delim indicates the end of a section in the Version 2 protocol.
	// https://git-scm.com/docs/protocol-v2#_packet_line_framing
	Delim Type = 1
	// Data indicates a packet with data.
	Data Type = 4
)

// Reader reads pkt-lines from an io.Reader object. It does not perform any
// internal buffering and does not read any more bytes than requested.
type Reader struct {
	r   io.Reader
	typ Type
	buf []byte
	err error
}

// NewReader returns a new Reader that reads from r.
func NewReader(r io.Reader) *Reader {
	return &Reader{
		r:   r,
		buf: make([]byte, 0, 1024),
	}
}

// Next advances the Reader to the next pkt-line, which will then be available
// through the Bytes or Text methods. It returns false when the Reader
// encounters an error. After Next returns false, the Err method will return any
// error that occurred while reading.
func (pr *Reader) Next() bool {
	if pr.err != nil {
		return false
	}
	pr.typ, pr.buf, pr.err = read(pr.r, pr.buf)
	return pr.err == nil
}

func read(r io.Reader, buf []byte) (_ Type, _ []byte, err error) {
	var lengthHex [4]byte
	if _, err := io.ReadFull(r, lengthHex[:]); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return Flush, buf[:0], fmt.Errorf("read packet line: %w", err)
	}
	var length [2]byte
	if _, err := hex.Decode(length[:], lengthHex[:]); err != nil {
		return Flush, buf[:0], fmt.Errorf("read packet line: invalid length: %w", err)
	}
	switch {
	case length[0] == 0 && length[1] == 0:
		return Flush, buf[:0], nil
	case length[0] == 0 && length[1] == 1:
		return Delim, buf[:0], nil
	case length[0] == 0 && length[1] < byte(len(lengthHex)):
		return Flush, buf[:0], fmt.Errorf("read packet line: invalid length %q", lengthHex)
	}
	n := int(length[0])<<8 | int(length[1]) - len(lengthHex)
	if n == 0 {
		// "Implementations SHOULD NOT send an empty pkt-line ("0004")."
		// ... but we're here for it.
		return Data, buf[:0], nil
	}
	if n > MaxSize {
		return Flush, buf[:0], fmt.Errorf("read packet line: invalid length %q", lengthHex)
	}
	if n > cap(buf) {
		buf = make([]byte, n)
	} else {
		buf = buf[:n]
	}
	if _, err := io.ReadFull(r, buf); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return Flush, buf[:0], fmt.Errorf("read packet line: %w", err)
	}
	return Data, buf, nil
}

// Type returns the type of the most recent packet read by a call to Next.
func (pr *Reader) Type() Type {
	return pr.typ
}

// Err returns the first error encountered by the Reader.
func (pr *Reader) Err() error {
	return pr.err
}

// Bytes returns the data of the most recent packet read by a call to Next.
// It returns an error if Next returned false or the most recent packet is not
// a Data packet. The underlying array may point to data that will be
// overwritten by a subsequent call to Next.
func (pr *Reader) Bytes() ([]byte, error) {
	if pr.err != nil {
		return nil, pr.err
	}
	if pr.typ != Data {
		return nil, fmt.Errorf("unexpected packet (want %d, got %d)", Data, pr.typ)
	}
	return pr.buf, nil
}

// Text returns the data of the most recent packet read by a call to Next,
// stripping the trailing line-feed ('\n') if present. It returns an error if
// Next returned false or the most recent packet is not a Data packet.
// The underlying array may point to data that will be overwritten by a
// subsequent call to Next.
func (pr *Reader) Text() ([]byte, error) {
	data, err := pr.Bytes()
	return trimLF(data), err
}

func trimLF(line []byte) []byte {
	if len(line) == 0 || line[len(line)-1] != '\n' {
		return line
	}
	return line[:len(line)-1]
}

// Append appends a data-pkt to dst with the given data. It panics if
// len(line) == 0 or len(line) > MaxSize.
func Append(dst []byte, line []byte) []byte {
	if len(line) == 0 {
		panic("empty pkt-line")
	}
	if len(line) > MaxSize {
		panic("pkt-line too large")
	}
	n := len(line) + 4
	dst = append(dst,
		hexDigits[n>>12],
		hexDigits[n>>8&0xf],
		hexDigits[n>>4&0xf],
		hexDigits[n&0xf],
	)
	dst = append(dst, line...)
	return dst
}

// AppendString appends a data-pkt to dst with the given data. It panics if
// len(line) == 0 or len(line) > MaxSize.
func AppendString(dst []byte, line string) []byte {
	if len(line) == 0 {
		panic("empty pkt-line")
	}
	if len(line) > MaxSize {
		panic("pkt-line too large")
	}
	n := len(line) + 4
	dst = append(dst,
		hexDigits[n>>12],
		hexDigits[n>>8&0xf],
		hexDigits[n>>4&0xf],
		hexDigits[n&0xf],
	)
	dst = append(dst, line...)
	return dst
}

// AppendFlush appends a flush-pkt to dst.
func AppendFlush(dst []byte) []byte {
	return append(dst, "0000"...)
}

// AppendDelim appends a delim-pkt to dst.
func AppendDelim(dst []byte) []byte {
	return append(dst, "0001"...)
}

const hexDigits = "0123456789abcdef"
