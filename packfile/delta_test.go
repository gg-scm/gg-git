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

	"gg-scm.io/pkg/git/object"
	"github.com/google/go-cmp/cmp"
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

func TestUndeltifier(t *testing.T) {
	tests := []struct {
		name  string
		setup func(io.Writer) (int64, object.Type, string, error)
	}{
		{
			name: "NonDeltaObject",
			setup: func(w io.Writer) (int64, object.Type, string, error) {
				pw := NewWriter(w, 1)
				const blobContent = "Hello, World!\n"
				offset, err := pw.WriteHeader(&Header{
					Type: Blob,
					Size: int64(len(blobContent)),
				})
				if err != nil {
					return 0, "", "", err
				}
				if _, err := pw.Write([]byte(blobContent)); err != nil {
					return 0, "", "", err
				}
				if err := pw.Close(); err != nil {
					return 0, "", "", err
				}
				return offset, object.TypeBlob, blobContent, nil
			},
		},
		{
			name: "OffsetDelta",
			setup: func(w io.Writer) (int64, object.Type, string, error) {
				pw := NewWriter(w, 2)
				const baseContent = "Hello, World!\n"
				baseOffset, err := pw.WriteHeader(&Header{
					Type: Blob,
					Size: int64(len(baseContent)),
				})
				if err != nil {
					return 0, "", "", err
				}
				if _, err := pw.Write([]byte(baseContent)); err != nil {
					return 0, "", "", err
				}

				const finalContent = "Hello, foo\n"
				delta := []byte{
					byte(len(baseContent)), // original size
					0x0b,                   // output size
					0b10010000,             // copy from base object
					0x07,                   // size1
					0x04,                   // add new data
					'f', 'o', 'o', '\n',
				}
				deltaOffset, err := pw.WriteHeader(&Header{
					Type:       OffsetDelta,
					BaseOffset: baseOffset,
					Size:       int64(len(delta)),
				})
				if err != nil {
					return 0, "", "", err
				}
				if _, err := pw.Write(delta); err != nil {
					return 0, "", "", err
				}
				if err := pw.Close(); err != nil {
					return 0, "", "", err
				}
				return deltaOffset, object.TypeBlob, finalContent, nil
			},
		},
		{
			name: "RefDelta",
			setup: func(w io.Writer) (int64, object.Type, string, error) {
				pw := NewWriter(w, 2)
				const baseContent = "Hello, World!\n"
				_, err := pw.WriteHeader(&Header{
					Type: Blob,
					Size: int64(len(baseContent)),
				})
				if err != nil {
					return 0, "", "", err
				}
				if _, err := pw.Write([]byte(baseContent)); err != nil {
					return 0, "", "", err
				}
				baseID, err := object.BlobSum(strings.NewReader(baseContent), int64(len(baseContent)))
				if err != nil {
					return 0, "", "", err
				}

				const finalContent = "Hello, foo\n"
				delta := []byte{
					byte(len(baseContent)), // original size
					0x0b,                   // output size
					0b10010000,             // copy from base object
					0x07,                   // size1
					0x04,                   // add new data
					'f', 'o', 'o', '\n',
				}
				deltaOffset, err := pw.WriteHeader(&Header{
					Type:       RefDelta,
					BaseObject: baseID,
					Size:       int64(len(delta)),
				})
				if err != nil {
					return 0, "", "", err
				}
				if _, err := pw.Write(delta); err != nil {
					return 0, "", "", err
				}
				if err := pw.Close(); err != nil {
					return 0, "", "", err
				}
				return deltaOffset, object.TypeBlob, finalContent, nil
			},
		},
	}

	for _, test := range tests {
		t.Run("Undeltify/"+test.name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			offset, wantType, wantData, err := test.setup(buf)
			if err != nil {
				t.Fatal(err)
			}
			idx, err := BuildIndex(bytes.NewReader(buf.Bytes()), int64(buf.Len()), nil)
			if err != nil {
				t.Fatal(err)
			}
			gotPrefix, gotReader, err := new(Undeltifier).Undeltify(bytes.NewReader(buf.Bytes()), offset, &UndeltifyOptions{
				Index: idx,
			})
			if err != nil {
				t.Fatal("Undeltify:", err)
			}
			if gotPrefix.Type != wantType {
				t.Errorf("prefix.Type = %q; want %q", gotPrefix.Type, wantType)
			}
			if gotPrefix.Size != int64(len(wantData)) {
				t.Errorf("prefix.Size = %d; want %d", gotPrefix.Size, len(wantData))
			}
			got := new(bytes.Buffer)
			if _, err := io.Copy(got, gotReader); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(wantData, got.String()); diff != "" {
				t.Errorf("content (-want +got):\n%s", diff)
			}
		})
	}

	for _, test := range tests {
		t.Run("ResolveType/"+test.name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			offset, want, _, err := test.setup(buf)
			if err != nil {
				t.Fatal(err)
			}
			idx, err := BuildIndex(bytes.NewReader(buf.Bytes()), int64(buf.Len()), nil)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ResolveType(bytes.NewReader(buf.Bytes()), offset, &UndeltifyOptions{
				Index: idx,
			})
			if got != want || err != nil {
				t.Errorf("ResolveType(f, %d) = %q, %v; want %q, <nil>", offset, got, err, want)
			}
		})
	}
}

func TestBufferedReadSeeker(t *testing.T) {
	const data = "Hello, World!\nfoobar\n"
	rs := NewBufferedReadSeekerSize(strings.NewReader(data), 16)
	if b, err := rs.ReadByte(); b != 'H' || err != nil {
		t.Errorf("rs.ReadByte()@0 = %q, %v; want 'H', <nil>", b, err)
	}

	got := make([]byte, 4)
	want := []byte(data[1 : 1+len(got)])
	n, err := io.ReadFull(rs, got)
	if err != nil {
		t.Errorf("io.ReadFull(rs, make([]byte, %d))@1 = %d, %v; want %d, <nil>", len(got), n, err, len(got))
	}
	if !bytes.Equal(got[:n], want) {
		t.Errorf("data@1 = %q; want %q", got[:n], want)
	}

	if pos, err := rs.Seek(2, io.SeekCurrent); pos != 7 || err != nil {
		t.Fatalf("rs.Seek(2, io.SeekCurrent)@%d = %d, %v; want 7, <nil>", 1+n, pos, err)
	}
	if b, err := rs.ReadByte(); b != 'W' || err != nil {
		t.Errorf("rs.ReadByte()@7 = %q, %v; want 'W', <nil>", b, err)
	}

	if pos, err := rs.Seek(9, io.SeekCurrent); pos != 17 || err != nil {
		t.Fatalf("rs.Seek(9, io.SeekCurrent)@8 = %d, %v; want 17, <nil>", pos, err)
	}
	if b, err := rs.ReadByte(); b != 'b' || err != nil {
		t.Errorf("rs.ReadByte()@17 = %q, %v; want 'b', <nil>", b, err)
	}

	if pos, err := rs.Seek(1, io.SeekStart); pos != 1 || err != nil {
		t.Fatalf("rs.Seek(1, io.SeekStart)@%d = %d, %v; want 1, <nil>", 1+n+3, pos, err)
	}
	n, err = io.ReadFull(rs, got)
	if err != nil {
		t.Errorf("io.ReadFull(rs, make([]byte, %d))@1 = %d, %v; want %d, <nil>", len(got), n, err, len(got))
	}
	if !bytes.Equal(got[:n], want) {
		t.Errorf("data@1 = %q; want %q", got[:n], want)
	}
}
