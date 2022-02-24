// Copyright 2020 The gg Authors
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

/*
Package object provides types for Git objects and functions for parsing and
serializing those objects. For an overview, see
https://git-scm.com/book/en/v2/Git-Internals-Git-Objects
*/
package object

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"io"
	"strconv"

	"gg-scm.io/pkg/git/githash"
)

// Type is an enumeration of Git object types.
type Type string

// Object types.
const (
	TypeBlob   Type = "blob"
	TypeTree   Type = "tree"
	TypeCommit Type = "commit"
	TypeTag    Type = "tag"
)

// IsValid reports whether typ is one of the known constants.
func (typ Type) IsValid() bool {
	return typ == TypeBlob || typ == TypeTree || typ == TypeCommit || typ == TypeTag
}

// BlobSum computes the Git SHA-1 object ID of the blob with the given content.
// This includes the Git object prefix as part of the hash input. It returns an
// error if the blob does not match the provided size in bytes.
func BlobSum(r io.Reader, size int64) (githash.SHA1, error) {
	h := sha1.New()
	h.Write(AppendPrefix(nil, TypeBlob, size))
	n, err := io.Copy(h, r)
	if err != nil {
		return githash.SHA1{}, fmt.Errorf("hash git blob: %w", err)
	}
	if n != size {
		return githash.SHA1{}, fmt.Errorf("hash git blob: wrong size %d (expected %d)", n, size)
	}
	var sum githash.SHA1
	h.Sum(sum[:0])
	return sum, nil
}

// Prefix is a parsed Git object prefix like "blob 42\x00".
type Prefix struct {
	Type Type
	Size int64
}

// MarshalBinary returns the result of AppendPrefix.
func (p Prefix) MarshalBinary() ([]byte, error) {
	if !p.Type.IsValid() {
		return nil, fmt.Errorf("marshal git object prefix: unknown type %q", p.Type)
	}
	if p.Size < 0 {
		return nil, fmt.Errorf("marshal git object prefix: negative size")
	}
	return AppendPrefix(nil, p.Type, p.Size), nil
}

// UnmarshalBinary parses an object prefix.
func (p *Prefix) UnmarshalBinary(data []byte) error {
	if len(data) == 0 || data[len(data)-1] != 0 {
		return fmt.Errorf("unmarshal git object prefix: does not end with NUL")
	}
	typeEnd := bytes.IndexByte(data, ' ')
	if typeEnd == -1 {
		return fmt.Errorf("unmarshal git object prefix: missing space")
	}
	typ := Type(data[:typeEnd])
	if !typ.IsValid() {
		return fmt.Errorf("unmarshal git object prefix: unknown type %q", typ)
	}
	sizeStart := typeEnd + 1
	sizeEnd := len(data) - 1
	size, err := strconv.ParseInt(string(data[sizeStart:sizeEnd]), 10, 64)
	if err != nil {
		return fmt.Errorf("unmarshal git object prefix: size: %v", err)
	}
	if size < 0 {
		return fmt.Errorf("unmarshal git object prefix: negative size")
	}
	p.Type = typ
	p.Size = size
	return nil
}

// String returns the prefix without the trailing NUL byte.
func (p Prefix) String() string {
	buf := AppendPrefix(nil, p.Type, p.Size)
	buf = buf[:len(buf)-1]
	return string(buf)
}

// AppendPrefix appends a Git object prefix (e.g. "blob 42\x00")
// to a byte slice.
func AppendPrefix(dst []byte, typ Type, n int64) []byte {
	dst = append(dst, typ...)
	dst = append(dst, ' ')
	dst = strconv.AppendInt(dst, n, 10)
	dst = append(dst, 0)
	return dst
}

func appendHex(dst, src []byte) []byte {
	const digits = "0123456789abcdef"
	for _, b := range src {
		dst = append(dst, digits[b>>4], digits[b&0xf])
	}
	return dst
}
