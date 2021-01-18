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
	"os"
	"path/filepath"
	"testing"

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
			got, err := BuildIndex(f, info.Size(), ObjectDir(t.TempDir()))
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
