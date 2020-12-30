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

package object

import (
	"strings"
	"testing"

	"gg-scm.io/pkg/git/githash"
)

func TestBlobSum(t *testing.T) {
	tests := []struct {
		data string
		want githash.SHA1
	}{
		{"", hashLiteral("e69de29bb2d1d6434b8b29ae775ad8c2e48c5391")},
		{"Hello, World!\n", hashLiteral("8ab686eafeb1f44702738c8b0f24f2567c36da6d")},
	}
	for _, test := range tests {
		size := int64(len(test.data))
		got, err := BlobSum(strings.NewReader(test.data), size)
		if got != test.want || err != nil {
			t.Errorf("BlobSum(strings.NewReader(%q), %d) = %v, %t; want %v, <nil>", test.data, size, got, err, test.want)
		}
	}

	t.Run("Short", func(t *testing.T) {
		_, err := BlobSum(strings.NewReader("foo"), 6)
		if err == nil {
			t.Fatal("BlobSum did not return an error")
		}
		t.Log("Error:", err)
	})

	t.Run("Long", func(t *testing.T) {
		_, err := BlobSum(strings.NewReader("foo"), 0)
		if err == nil {
			t.Fatal("BlobSum did not return an error")
		}
		t.Log("Error:", err)
	})
}

func hashLiteral(s string) githash.SHA1 {
	var h githash.SHA1
	if err := h.UnmarshalText([]byte(s)); err != nil {
		panic(err)
	}
	return h
}
