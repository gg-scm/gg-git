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
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestWriter(t *testing.T) {
	for _, test := range testFiles {
		t.Run(test.name, func(t *testing.T) {
			out := new(bytes.Buffer)
			w := NewWriter(out, uint32(len(test.want)))
			want := make([]unpackedObject, 0, len(test.want))
			offsetMap := make(map[int64]int64)
			for i, obj := range test.want {
				hdr := obj.Header
				if obj.BaseOffset != 0 {
					hdr = new(Header)
					*hdr = *obj.Header
					hdr.BaseOffset = offsetMap[obj.BaseOffset]
					if hdr.BaseOffset == 0 {
						t.Errorf("BaseOffset %d failed to remap", obj.BaseOffset)
					}
				}
				offset, err := w.WriteHeader(hdr)
				if err != nil {
					t.Errorf("[%d] w.WriteHeader(...): %v", i, err)
					continue
				}
				if _, err := w.Write(obj.Data); err != nil {
					t.Errorf("[%d] w.Write(...): %v", i, err)
				}
				// Remap to new offset.
				newobj := obj
				newobj.Offset = offset
				want = append(want, newobj)
				offsetMap[obj.Offset] = offset
			}
			if err := w.Close(); err != nil {
				t.Error(err)
			}

			got, err := readAll(out)
			if err != nil {
				t.Fatal("Read:", err)
			}
			if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("objects (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAppendLengthType(t *testing.T) {
	tests := []struct {
		name string
		typ  ObjectType
		n    int64
		want []byte
	}{
		{
			name: "ZeroBlob",
			typ:  Blob,
			n:    0,
			want: []byte{0x30},
		},
		{
			name: "SmallBlob",
			typ:  Blob,
			n:    10,
			want: []byte{0x3a},
		},
		{
			name: "MediumBlob",
			typ:  Blob,
			n:    42,
			want: []byte{0xba, 0x02},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := appendLengthType(nil, test.typ, test.n)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("appendLengthType(nil, %d, %d) (-want +got):\n%s", int(test.typ), test.n, diff)
			}
		})
	}
}

func TestAppendVarint(t *testing.T) {
	tests := []uint64{
		0x00,
		0x01,
		0x7f,
		0xff,
		0xffffffffffffffff,
	}
	for _, x := range tests {
		want := make([]byte, binary.MaxVarintLen64)
		wantN := binary.PutUvarint(want, x)
		want = want[:wantN]

		got := appendVarint(nil, x)
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("appendVarint(nil, %#x) (-want +got):\n%s", x, diff)
		}
	}
}

func TestAppendOffset(t *testing.T) {
	for _, test := range offsetTests {
		got := appendOffset(nil, test.offset)
		if diff := cmp.Diff(test.data, got); diff != "" {
			t.Errorf("appendOffset(nil, %d) (-want +got):\n%s", test.offset, diff)
		}
	}
}
