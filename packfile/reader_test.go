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
	"bufio"
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"gg-scm.io/pkg/git/githash"
	"github.com/google/go-cmp/cmp"
)

type unpackedObject struct {
	*Header
	Data []byte
}

var testFiles = []struct {
	name      string
	want      []unpackedObject
	wantIndex *Index
	wantError bool
}{
	{
		name: "Empty",
		wantIndex: &Index{
			PackfileSHA1: hashLiteral("029d08823bd8a8eab510ad6ac75c823cfd3ed31e"),
		},
	},
	{
		name: "FirstCommit",
		want: []unpackedObject{
			{
				Header: &Header{
					Offset: 12,
					Type:   Blob,
					Size:   14,
				},
				Data: []byte("Hello, World!\n"),
			},
			{
				Header: &Header{
					Offset: 39,
					Type:   Tree,
					Size:   37,
				},
				Data: []byte("100644 hello.txt\x00" +
					"\x8a\xb6\x86\xea\xfe\xb1\xf4\x47\x02\x73" +
					"\x8c\x8b\x0f\x24\xf2\x56\x7c\x36\xda\x6d"),
			},
			{
				Header: &Header{
					Offset: 91,
					Type:   Commit,
					Size:   171,
				},
				Data: []byte("tree bc225ea23f53f06c0c5bd3ba2be85c2120d68417\n" +
					"author Octocat <octocat@example.com> 1608391559 -0800\n" +
					"committer Octocat <octocat@example.com> 1608391559 -0800\n" +
					"\n" +
					"First commit\n"),
			},
		},
		wantIndex: &Index{
			Offsets: []int64{12, 91, 39},
			ObjectIDs: []githash.SHA1{
				hashLiteral("8ab686eafeb1f44702738c8b0f24f2567c36da6d"),
				hashLiteral("aef8a4c3fe8d296dec2d9b88d4654cd596927867"),
				hashLiteral("bc225ea23f53f06c0c5bd3ba2be85c2120d68417"),
			},
			PackedChecksums: []uint32{
				0xd6402b58,
				0x8f92a93a,
				0x7fa848c1,
			},
			PackfileSHA1: hashLiteral("6d08a5bf64e27c0ef29448d8e50d56369b17198f"),
		},
	},
	{
		name: "DeltaOffset",
		want: []unpackedObject{
			{
				Header: &Header{
					Offset: 12,
					Type:   Blob,
					Size:   6,
				},
				Data: []byte("Hello!"),
			},
			{
				Header: &Header{
					Offset:     31,
					Type:       OffsetDelta,
					Size:       13,
					BaseOffset: 12,
				},
				Data: helloDelta,
			},
		},
		wantIndex: &Index{
			Offsets: []int64{12, 31},
			ObjectIDs: []githash.SHA1{
				hashLiteral("05a682bd4e7c7117c5856be7142fea67465415e3"),
				hashLiteral("45c3b785642598057cf65b79fd05586dae5cba10"),
			},
			PackedChecksums: []uint32{
				0x1d0344fe,
				0x82c20b92,
			},
			PackfileSHA1: hashLiteral("fe67ec299ad01178f132db12d7bf93fe9897a646"),
		},
	},
	{
		name: "DeltaObject",
		want: []unpackedObject{
			{
				Header: &Header{
					Offset: 12,
					Type:   Blob,
					Size:   6,
				},
				Data: []byte("Hello!"),
			},
			{
				Header: &Header{
					Offset:     31,
					Type:       RefDelta,
					Size:       13,
					BaseObject: hashLiteral("05a682bd4e7c7117c5856be7142fea67465415e3"),
				},
				Data: helloDelta,
			},
		},
		wantIndex: &Index{
			Offsets: []int64{12, 31},
			ObjectIDs: []githash.SHA1{
				hashLiteral("05a682bd4e7c7117c5856be7142fea67465415e3"),
				hashLiteral("45c3b785642598057cf65b79fd05586dae5cba10"),
			},
			PackedChecksums: []uint32{
				0x1d0344fe,
				0xf9c7e1ee,
			},
			PackfileSHA1: hashLiteral("5ca6b70287d79e571d8a86b6652cc351028f0658"),
		},
	},
	{
		name: "EmptyBlob",
		want: []unpackedObject{
			{
				Header: &Header{
					Offset: 12,
					Type:   Blob,
					Size:   0,
				},
				Data: []byte{},
			},
			{
				Header: &Header{
					Offset: 24,
					Type:   Blob,
					Size:   14,
				},
				Data: []byte("Hello, World!\n"),
			},
		},
		wantIndex: &Index{
			Offsets: []int64{24, 12},
			ObjectIDs: []githash.SHA1{
				hashLiteral("8ab686eafeb1f44702738c8b0f24f2567c36da6d"),
				hashLiteral("e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"),
			},
			PackedChecksums: []uint32{
				0xd6402b58,
				0xbe56632f,
			},
			PackfileSHA1: hashLiteral("1fb6c9a5c90236ff883be04f3c5796435b9a6569"),
		},
	},
	{
		name: "TooLong",
		want: []unpackedObject{
			{
				Header: &Header{
					Offset: 12,
					Type:   Blob,
					Size:   1,
				},
				Data: []byte("H"),
			},
		},
		wantError: true,
	},
	{
		name: "TooShort",
		want: []unpackedObject{
			{
				Header: &Header{
					Offset: 12,
					Type:   Blob,
					Size:   6,
				},
				Data: []byte("Hello"),
			},
		},
		wantError: true,
	},
}

// helloDelta is the set of instructions to transform "Hello!" into "Hello, delta\n".
var helloDelta = []byte{
	0x06,       // original size
	0x0d,       // output size
	0b10010000, // copy from base, offset 0, one size byte
	0x05,       // size1
	0x08,       // add new data (length 8)
	',', ' ', 'd', 'e', 'l', 't', 'a', '\n',
}

func TestReader(t *testing.T) {
	for _, test := range testFiles {
		t.Run(test.name, func(t *testing.T) {
			f, err := os.Open(filepath.Join("testdata", test.name+".pack"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			got, err := readAll(bufio.NewReader(f))
			if err != nil {
				t.Log("Error:", err)
				if !test.wantError {
					t.Fail()
				}
			} else if test.wantError {
				t.Error("No error returned")
			}
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("objects (-want +got):\n%s", diff)
			}
		})
	}
}

func readAll(br ByteReader) ([]unpackedObject, error) {
	r := NewReader(br)
	var got []unpackedObject
	for {
		hdr, err := r.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = nil
			}
			return got, err
		}
		data, err := ioutil.ReadAll(r)
		got = append(got, unpackedObject{
			Header: hdr,
			Data:   data,
		})
		if err != nil {
			return got, err
		}
	}
}

var offsetTests = []struct {
	data   []byte
	offset int64
}{
	{[]byte{0x00}, -0},
	{[]byte{0x4a}, -74},
	{[]byte{0x80, 0x00}, -128},
	{[]byte{0x81, 0x00}, -256},
	{[]byte{0x92, 0x29}, -2473},
	{[]byte{0x86, 0x40}, -960},
	{[]byte{0x80, 0xe5, 0x2d}, -29485},
}

func TestReadOffset(t *testing.T) {
	for _, test := range offsetTests {
		got, err := readOffset(bytes.NewReader(test.data))
		if got != test.offset || err != nil {
			t.Errorf("readOffset(bytes.NewReader(%#v)) = %d, %v; want %d, <nil>", test.data, got, err, test.offset)
		}
	}
}

func hashLiteral(s string) githash.SHA1 {
	var h githash.SHA1
	if err := h.UnmarshalText([]byte(s)); err != nil {
		panic(err)
	}
	return h
}
