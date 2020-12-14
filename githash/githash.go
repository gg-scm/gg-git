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

// Package githash provides a type for Git object hashes.
package githash

import (
	"encoding/hex"
	"fmt"
	"io"
)

// SHA1Size is the number of bytes in a SHA-1 hash.
const SHA1Size = 20

// A SHA1 is the SHA-1 hash of a Git object.
type SHA1 [SHA1Size]byte

// ParseSHA1 parses a hex-encoded SHA-1 hash. It is the same as calling
// UnmarshalText on a new SHA1.
func ParseSHA1(s string) (SHA1, error) {
	var h SHA1
	err := h.UnmarshalText([]byte(s))
	return h, err
}

// String returns the hex-encoded hash.
func (h SHA1) String() string {
	return hex.EncodeToString(h[:])
}

// Short returns the first 4 hex-encoded bytes of the hash.
func (h SHA1) Short() string {
	return hex.EncodeToString(h[:4])
}

// MarshalText returns the hex-encoded hash.
func (h SHA1) MarshalText() ([]byte, error) {
	buf := make([]byte, hex.EncodedLen(len(h)))
	hex.Encode(buf, h[:])
	return buf, nil
}

// UnmarshalText decodes a hex-encoded hash into h.
func (h *SHA1) UnmarshalText(s []byte) error {
	if len(s) != hex.EncodedLen(SHA1Size) {
		return fmt.Errorf("parse git hash %q: wrong size", s)
	}
	if _, err := hex.Decode(h[:], s); err != nil {
		return fmt.Errorf("parse git hash %q: %w", s, err)
	}
	return nil
}

// MarshalBinary returns the hash as a byte slice.
func (h SHA1) MarshalBinary() ([]byte, error) {
	return h[:], nil
}

// UnmarshalBinary copies the bytes from b into h. It returns an error if
// len(b) != len(*h).
func (h *SHA1) UnmarshalBinary(b []byte) error {
	if len(b) != len(*h) {
		return fmt.Errorf("parse git binary hash %x: wrong size", b)
	}
	copy(h[:], b)
	return nil
}

// Format implements the fmt.Formatter interface.
// Specifically, it ensures that %x does not double-hex-encode the data.
func (h SHA1) Format(f fmt.State, c rune) {
	bits := h[:]
	if prec, ok := f.Precision(); ok && c != 'v' && prec < len(bits) {
		bits = bits[:prec]
	}
	text := make([]byte, hex.EncodedLen(len(bits)))
	hex.Encode(text, bits)

	switch c {
	case 's':
		f.Write(text)
	case 'v':
		if !f.Flag('#') {
			f.Write(text)
			return
		}
		f.Write([]byte("githash.SHA1{"))
		sep := []byte(", 0x")
		f.Write(sep[2:])
		f.Write(text[:2])
		for i := 2; i < len(text); i += 2 {
			f.Write(sep)
			f.Write(text[i : i+2])
		}
		f.Write([]byte("}"))
	case 'x':
		if f.Flag('#') {
			f.Write([]byte("0x"))
		}
		f.Write(text)
	case 'X':
		if f.Flag('#') {
			f.Write([]byte("0X"))
		}
		for i, c := range text {
			if 'a' <= c && c <= 'f' {
				text[i] = c - 'a' + 'A'
			}
		}
		f.Write(text)
	default:
		// Print a wrong type/unknown verb error.
		f.Write([]byte("%!"))
		io.WriteString(f, string(c))
		f.Write([]byte("(githash.SHA1="))
		f.Write(text)
		f.Write([]byte(")"))
	}
}
