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
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gg-scm.io/pkg/git/githash"
	"gg-scm.io/pkg/git/object"
	"gg-scm.io/pkg/git/packfile"
)

func Example() {
	// Open a packfile.
	file, err := os.Open(filepath.Join("testdata", "DeltaObject.pack"))
	if err != nil {
		// handle error
	}
	fileInfo, err := file.Stat()
	if err != nil {
		// handle error
	}

	// Index the packfile.
	idx, err := packfile.BuildIndex(file, fileInfo.Size(), nil)
	if err != nil {
		// handle error
	}

	// Find the position of an object.
	commitID, err := githash.ParseSHA1("45c3b785642598057cf65b79fd05586dae5cba10")
	if err != nil {
		// handle error
	}
	i := idx.FindID(commitID)
	if i == -1 {
		// handle not-found error
	}

	// Read the object from the packfile.
	undeltifier := new(packfile.Undeltifier)
	bufferedFile := packfile.NewBufferedReadSeeker(file)
	prefix, content, err := undeltifier.Undeltify(bufferedFile, idx.Offsets[i], &packfile.UndeltifyOptions{
		Index: idx,
	})
	if err != nil {
		// handle error
	}
	fmt.Println(prefix)
	io.Copy(os.Stdout, content)

	// Output:
	// blob 13
	// Hello, delta
}

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

func ExampleIndex() {
	// Open a packfile.
	file, err := os.Open(filepath.Join("testdata", "FirstCommit.pack"))
	if err != nil {
		// handle error
	}
	fileInfo, err := file.Stat()
	if err != nil {
		// handle error
	}

	// Index the packfile.
	idx, err := packfile.BuildIndex(file, fileInfo.Size(), nil)
	if err != nil {
		// handle error
	}

	// Print a sorted list of all objects in the packfile.
	for _, id := range idx.ObjectIDs {
		fmt.Println(id)
	}

	// Output:
	// 8ab686eafeb1f44702738c8b0f24f2567c36da6d
	// aef8a4c3fe8d296dec2d9b88d4654cd596927867
	// bc225ea23f53f06c0c5bd3ba2be85c2120d68417
}

func ExampleWriter() {
	// Create a writer.
	buf := new(bytes.Buffer)
	const objectCount = 3
	writer := packfile.NewWriter(buf, objectCount)

	// Write a blob.
	const blobContent = "Hello, World!\n"
	_, err := writer.WriteHeader(&packfile.Header{
		Type: packfile.Blob,
		Size: int64(len(blobContent)),
	})
	if err != nil {
		// handle error
	}
	if _, err := io.WriteString(writer, blobContent); err != nil {
		// handle error
	}
	blobSum, err := object.BlobSum(strings.NewReader(blobContent), int64(len(blobContent)))
	if err != nil {
		// handle error
	}

	// Write a tree (directory).
	tree := object.Tree{
		{Name: "hello.txt", Mode: object.ModePlain, ObjectID: blobSum},
	}
	treeData, err := tree.MarshalBinary()
	if err != nil {
		// handle error
	}
	_, err = writer.WriteHeader(&packfile.Header{
		Type: packfile.Tree,
		Size: int64(len(treeData)),
	})
	if err != nil {
		// handle error
	}
	if _, err := writer.Write(treeData); err != nil {
		// handle error
	}

	// Write a commit.
	const user object.User = "Octocat <octocat@example.com>"
	commitTime := time.Unix(1608391559, 0).In(time.FixedZone("-0800", -8*60*60))
	commit := &object.Commit{
		Tree:       tree.SHA1(),
		Author:     user,
		AuthorTime: commitTime,
		Committer:  user,
		CommitTime: commitTime,
		Message:    "First commit\n",
	}
	commitData, err := commit.MarshalBinary()
	if err != nil {
		// handle error
	}
	_, err = writer.WriteHeader(&packfile.Header{
		Type: packfile.Commit,
		Size: int64(len(commitData)),
	})
	if err != nil {
		// handle error
	}
	if _, err := writer.Write(commitData); err != nil {
		// handle error
	}

	// Finish the write.
	if err := writer.Close(); err != nil {
		// handle error
	}
}
