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

package packfile

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestDeltaReader(t *testing.T) {
	tests := []struct {
		name  string
		base  string
		delta []byte
		want  string
	}{
		{
			name: "Empty",
			delta: []byte{
				0x00, // original size
				0x00, // output size
			},
		},
		{
			name: "CopyAll",
			base: "Hello",
			delta: []byte{
				0x05,       // original size
				0x05,       // output size
				0b10010000, // copy from base object
				0x05,       // size1
			},
			want: "Hello",
		},
		{
			name:  "Hello",
			base:  "Hello!",
			delta: helloDelta,
			want:  "Hello, delta\n",
		},
		{
			name: "OffsetCopy",
			base: "Hello",
			delta: []byte{
				0x05,       // original size
				0x03,       // output size
				0b10010001, // copy from base object
				0x01,       // offset1
				0x03,       // size1
			},
			want: "ell",
		},
		{
			name: "ZeroSizeCopy",
			base: strings.Repeat("x", 0x10000),
			delta: []byte{
				0x80, 0x80, 0x80, 0x80, 0x10, // original size
				0x80, 0x80, 0x80, 0x80, 0x10, // output size
				0b10000000, // copy from base object
			},
			want: strings.Repeat("x", 0x10000),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := new(bytes.Buffer)
			d := NewDeltaReader(strings.NewReader(test.base), bytes.NewReader(test.delta))
			if n, err := io.Copy(got, d); err != nil {
				t.Errorf("io.Copy(...) = %d, %v; want %d, <nil>", n, err, len(test.want))
			}
			if got.String() != test.want {
				t.Errorf("got %q; want %q", got, test.want)
			}

			t.Run("Size", func(t *testing.T) {
				n, err := DeltaObjectSize(bytes.NewReader(test.delta))
				if n != int64(len(test.want)) || err != nil {
					t.Errorf("DeltaObjectSize(...) = %d, %v; want %d, <nil>", n, err, len(test.want))
				}
			})
		})
	}
}
