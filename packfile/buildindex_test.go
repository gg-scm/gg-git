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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gg-scm.io/pkg/git/githash"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestBuildIndex(t *testing.T) {
	for _, test := range testFiles {
		t.Run(test.name, func(t *testing.T) {
			f, err := os.Open(filepath.Join("testdata", test.name+".pack"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				t.Fatal(err)
			}
			got, err := BuildIndex(f, info.Size(), nil)
			if err != nil {
				t.Log("Error:", err)
				if !test.wantError {
					t.Fail()
				}
			} else if test.wantError {
				t.Error("No error returned")
			}
			if diff := cmp.Diff(test.wantIndex, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("index (-want +got):\n%s", diff)
			}
		})
	}
}

func BenchmarkBuildIndex(b *testing.B) {
	buf := new(bytes.Buffer)
	w := NewWriter(buf, uint32(b.N))
	for i := 0; i < b.N; i++ {
		data := fmt.Sprintf("blob %10d\n", i)
		_, err := w.WriteHeader(&Header{
			Type: Blob,
			Size: int64(len(data)),
		})
		if err != nil {
			b.Fatal(err)
		}
		if _, err := w.Write([]byte(data)); err != nil {
			b.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()

	_, err := BuildIndex(bytes.NewReader(buf.Bytes()), int64(buf.Len()), nil)
	if err != nil {
		b.Fatal(err)
	}
	objectByteCount := buf.Len() - githash.SHA1Size - fileHeaderSize
	b.SetBytes(int64(float64(objectByteCount) / float64(b.N)))
	b.ReportMetric(float64(objectByteCount), "packfile-bytes")
}
