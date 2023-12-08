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

package packfile

import (
	"bytes"
	"encoding"
	"os"
	"path/filepath"
	"testing"

	"gg-scm.io/pkg/git/githash"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var (
	_ encoding.BinaryMarshaler   = new(Index)
	_ encoding.BinaryUnmarshaler = new(Index)
)

var bigOffsetIndex = &Index{
	Offsets: []int64{
		0x1_0000_0018,
		0x1_0000_000c,
	},
	ObjectIDs: []githash.SHA1{
		hashLiteral("8ab686eafeb1f44702738c8b0f24f2567c36da6d"),
		hashLiteral("e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"),
	},
	PackedChecksums: []uint32{
		0xd6402b58,
		0xbe56632f,
	},
	PackfileSHA1: hashLiteral("1fb6c9a5c90236ff883be04f3c5796435b9a6569"),
}

func TestReadIndex(t *testing.T) {
	for _, test := range testFiles {
		if test.wantError {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			t.Run("Version1", func(t *testing.T) {
				f, err := os.Open(filepath.Join("testdata", test.name+".idx1"))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				got, err := ReadIndex(f)
				if err != nil {
					t.Error("ReadIndex:", err)
				}
				diff := cmp.Diff(test.wantIndex, got,
					cmpopts.EquateEmpty(),
					// Version 1 index files do not include packed checksums.
					cmpopts.IgnoreFields(Index{}, "PackedChecksums"),
				)
				if diff != "" {
					t.Errorf("index (-want +got):\n%s", diff)
				}
				if got != nil && got.PackedChecksums != nil {
					t.Errorf("index has %d packed checksums; want <nil>", len(got.PackedChecksums))
				}
			})

			t.Run("Version2", func(t *testing.T) {
				f, err := os.Open(filepath.Join("testdata", test.name+".idx2"))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				got, err := ReadIndex(f)
				if err != nil {
					t.Error("ReadIndex:", err)
				}
				if diff := cmp.Diff(test.wantIndex, got, cmpopts.EquateEmpty()); diff != "" {
					t.Errorf("index (-want +got):\n%s", diff)
				}
			})
		})
	}

	t.Run("BigOffset", func(t *testing.T) {
		f, err := os.Open(filepath.Join("testdata", "BigOffset.idx2"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		got, err := ReadIndex(f)
		if err != nil {
			t.Error("ReadIndex:", err)
		}
		if diff := cmp.Diff(bigOffsetIndex, got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("index (-want +got):\n%s", diff)
		}
	})
}

func TestIndexEncodeV1(t *testing.T) {
	for _, test := range testFiles {
		if test.wantError {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", test.name+".idx1"))
			if err != nil {
				t.Fatal(err)
			}
			got := new(bytes.Buffer)
			if err := test.wantIndex.EncodeV1(got); err != nil {
				t.Error("EncodeV1:", err)
			}
			if diff := cmp.Diff(want, got.Bytes()); diff != "" {
				t.Errorf("index (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("Nil", func(t *testing.T) {
		want, err := os.ReadFile(filepath.Join("testdata", "Empty.idx1"))
		if err != nil {
			t.Fatal(err)
		}
		got := new(bytes.Buffer)
		if err := (*Index)(nil).EncodeV1(got); err != nil {
			t.Error("EncodeV1:", err)
		}
		if diff := cmp.Diff(want, got.Bytes()); diff != "" {
			t.Errorf("index (-want +got):\n%s", diff)
		}
	})
}

func TestIndexEncodeV2(t *testing.T) {
	for _, test := range testFiles {
		if test.wantError {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", test.name+".idx2"))
			if err != nil {
				t.Fatal(err)
			}
			got := new(bytes.Buffer)
			if err := test.wantIndex.EncodeV2(got); err != nil {
				t.Error("EncodeV2:", err)
			}
			if diff := cmp.Diff(want, got.Bytes()); diff != "" {
				t.Errorf("index (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("BigOffset", func(t *testing.T) {
		want, err := os.ReadFile(filepath.Join("testdata", "BigOffset.idx2"))
		if err != nil {
			t.Fatal(err)
		}
		got := new(bytes.Buffer)
		if err := bigOffsetIndex.EncodeV2(got); err != nil {
			t.Error("EncodeV2:", err)
		}
		if diff := cmp.Diff(want, got.Bytes()); diff != "" {
			t.Errorf("index (-want +got):\n%s", diff)
		}
	})

	t.Run("Nil", func(t *testing.T) {
		want, err := os.ReadFile(filepath.Join("testdata", "Empty.idx2"))
		if err != nil {
			t.Fatal(err)
		}
		got := new(bytes.Buffer)
		if err := (*Index)(nil).EncodeV2(got); err != nil {
			t.Error("EncodeV2:", err)
		}
		if diff := cmp.Diff(want, got.Bytes()); diff != "" {
			t.Errorf("index (-want +got):\n%s", diff)
		}
	})
}
