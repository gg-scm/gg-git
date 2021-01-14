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

package packfile_test

import (
	"bufio"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gg-scm.io/pkg/git/packfile"
)

// This example uses ReadHeader to perform random access in a packfile.
func ExampleReadHeader() {
	// Open a packfile.
	f, err := os.Open(filepath.Join("testdata", "FirstCommit.pack"))
	if err != nil {
		// handle error
	}

	// Seek to a specific index. You can get this from an index or previous read.
	const offset = 12
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		// handle error
	}

	// Read the object and its header.
	reader := bufio.NewReader(f)
	hdr, err := packfile.ReadHeader(offset, reader)
	if err != nil {
		// handle error
	}
	fmt.Println(hdr.Type)
	// The object is zlib-compressed in the packfile after the header.
	zreader, err := zlib.NewReader(reader)
	if err != nil {
		// handle error
	}
	if _, err := io.Copy(os.Stdout, zreader); err != nil {
		// handle error
	}

	// Output:
	// OBJ_BLOB
	// Hello, World!
}
