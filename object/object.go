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
	"crypto/sha1"
	"fmt"
	"io"
	"strconv"

	"gg-scm.io/pkg/git/githash"
)

// BlobSum computes the Git SHA-1 object ID of the blob with the given content.
// This includes the Git object prefix as part of the hash input. It returns an
// error if the blob does not match the provided size in bytes.
func BlobSum(r io.Reader, size int64) (githash.SHA1, error) {
	h := sha1.New()
	h.Write(AppendPrefix(nil, "blob", size))
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

// AppendPrefix appends a Git object prefix (e.g. "blob 42\x00")
// to a byte slice.
func AppendPrefix(dst []byte, typ string, n int64) []byte {
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
