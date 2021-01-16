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

// +build ignore

package main

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"gg-scm.io/pkg/git/object"
	"gg-scm.io/pkg/git/packfile"
)

func main() {
	funcMap := map[string]func() error{
		"Empty":       empty,
		"FirstCommit": firstCommit,
		"DeltaOffset": deltaOffset,
		"DeltaObject": deltaObject,
		"EmptyBlob":   emptyBlob,
		"TooLong":     tooLong,
		"TooShort":    tooShort,
	}
	var names []string
	for k := range funcMap {
		names = append(names, k)
	}
	sort.Strings(names)
	if len(os.Args) < 2 {
		for _, k := range names {
			fmt.Println(k)
		}
		return
	}
	f := funcMap[os.Args[1]]
	if len(os.Args) > 2 || f == nil {
		fmt.Fprint(os.Stderr, "usage: genpack ")
		for i, k := range names {
			if i > 0 {
				fmt.Fprint(os.Stderr, "|")
			}
			fmt.Fprint(os.Stderr, k)
		}
		fmt.Fprintln(os.Stderr)
		os.Exit(64)
	}

	if err := f(); err != nil {
		fmt.Fprintln(os.Stderr, "genpack:", err)
		os.Exit(1)
	}
}

func empty() error {
	w := packfile.NewWriter(os.Stdout, 0)
	return w.Close()
}

func firstCommit() (err error) {
	w := packfile.NewWriter(os.Stdout, 3)
	defer func() {
		if closeErr := w.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	const blobContent = "Hello, World!\n"
	_, err = w.WriteHeader(&packfile.Header{
		Type: packfile.Blob,
		Size: int64(len(blobContent)),
	})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, blobContent); err != nil {
		return err
	}
	blobHash := hashObject(object.TypeBlob, []byte(blobContent))
	fmt.Fprintf(os.Stderr, "blob = %02x\n", blobHash[:])

	treeBuf := []byte("100644 hello.txt\x00")
	treeBuf = append(treeBuf, blobHash[:]...)
	_, err = w.WriteHeader(&packfile.Header{
		Type: packfile.Tree,
		Size: int64(len(treeBuf)),
	})
	if err != nil {
		return err
	}
	if _, err := w.Write(treeBuf); err != nil {
		return err
	}
	treeHash := hashObject(object.TypeTree, treeBuf)
	fmt.Fprintf(os.Stderr, "tree = %02x\n", treeHash[:])

	commitBuf := new(bytes.Buffer)
	fmt.Fprintf(commitBuf, "tree %02x\n", treeHash[:])
	const unixTimestamp = 1608391559
	fmt.Fprintf(commitBuf, "author Octocat <octocat@example.com> %d -0800\n", unixTimestamp)
	fmt.Fprintf(commitBuf, "committer Octocat <octocat@example.com> %d -0800\n", unixTimestamp)
	fmt.Fprintf(commitBuf, "\n")
	fmt.Fprintf(commitBuf, "First commit\n")
	_, err = w.WriteHeader(&packfile.Header{
		Type: packfile.Commit,
		Size: int64(commitBuf.Len()),
	})
	if err != nil {
		return err
	}
	commitHash := hashObject(object.TypeCommit, commitBuf.Bytes())
	fmt.Fprintf(os.Stderr, "commit = %02x\n", commitHash[:])
	if _, err := io.Copy(w, commitBuf); err != nil {
		return err
	}

	return nil
}

func deltaOffset() (err error) {
	w := packfile.NewWriter(os.Stdout, 2)
	defer func() {
		if closeErr := w.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	const baseContent = "Hello!"
	baseOffset, err := w.WriteHeader(&packfile.Header{
		Type: packfile.Blob,
		Size: int64(len(baseContent)),
	})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, baseContent); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "baseOffset = %#x\n", baseOffset)

	deltaContent := []byte{
		0x06,       // original size
		0x0d,       // output size
		0b10010000, // copy from base, offset 0, one size byte
		0x05,       // size1
		0x08,       // add new data (length 8)
		',', ' ', 'd', 'e', 'l', 't', 'a', '\n',
	}
	const blobContent = "Hello, delta\n"
	if err := validateDelta(blobContent, baseContent, deltaContent); err != nil {
		return err
	}
	deltaObjectOffset, err := w.WriteHeader(&packfile.Header{
		Type:       packfile.OffsetDelta,
		Size:       int64(len(deltaContent)),
		BaseOffset: baseOffset,
	})
	if err != nil {
		return err
	}
	if _, err := w.Write(deltaContent); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "deltaObjectOffset = %#x\n", deltaObjectOffset)

	blobHash := hashObject(object.TypeBlob, []byte(blobContent))
	fmt.Fprintf(os.Stderr, "blob = %02x\n", blobHash[:])
	return nil
}

func deltaObject() (err error) {
	w := packfile.NewWriter(os.Stdout, 2)
	defer func() {
		if closeErr := w.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	const baseContent = "Hello!"
	baseOffset, err := w.WriteHeader(&packfile.Header{
		Type: packfile.Blob,
		Size: int64(len(baseContent)),
	})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, baseContent); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "baseOffset = %#x\n", baseOffset)
	baseBlobHash := hashObject(object.TypeBlob, []byte(baseContent))
	fmt.Fprintf(os.Stderr, "base blob = %02x\n", baseBlobHash)

	deltaContent := []byte{
		0x06,       // original size
		0x0d,       // output size
		0b10010000, // copy from base, offset 0, one size byte
		0x05,       // size1
		0x08,       // add new data (length 8)
		',', ' ', 'd', 'e', 'l', 't', 'a', '\n',
	}
	const blobContent = "Hello, delta\n"
	if err := validateDelta(blobContent, baseContent, deltaContent); err != nil {
		return err
	}
	deltaObjectOffset, err := w.WriteHeader(&packfile.Header{
		Type:       packfile.RefDelta,
		Size:       int64(len(deltaContent)),
		BaseObject: baseBlobHash,
	})
	if err != nil {
		return err
	}
	if _, err := w.Write(deltaContent); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "deltaObjectOffset = %#x\n", deltaObjectOffset)

	blobHash := hashObject(object.TypeBlob, []byte(blobContent))
	fmt.Fprintf(os.Stderr, "blob = %02x\n", blobHash[:])
	return nil
}

func validateDelta(want, base string, delta []byte) error {
	buf := new(bytes.Buffer)
	d := packfile.NewDeltaReader(strings.NewReader(base), bytes.NewReader(delta))
	if _, err := io.Copy(buf, d); err != nil {
		return fmt.Errorf("validate delta: %w", err)
	}
	if !bytes.Equal(buf.Bytes(), []byte(want)) {
		return fmt.Errorf("validate delta: does not produce expected data (got %q; want %q)", buf, want)
	}
	return nil
}

func emptyBlob() (err error) {
	w := packfile.NewWriter(os.Stdout, 2)
	defer func() {
		if closeErr := w.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	// Empty blob
	_, err = w.WriteHeader(&packfile.Header{
		Type: packfile.Blob,
		Size: 0,
	})
	if err != nil {
		return err
	}

	// One blob after
	const blobContent = "Hello, World!\n"
	_, err = w.WriteHeader(&packfile.Header{
		Type: packfile.Blob,
		Size: int64(len(blobContent)),
	})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, blobContent); err != nil {
		return err
	}

	return nil
}

func tooLong() (err error) {
	// You will need to comment out integrity checks in the writer to have this run.
	w := packfile.NewWriter(os.Stdout, 1)
	defer func() {
		if closeErr := w.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	const blobContent = "Hi"
	_, err = w.WriteHeader(&packfile.Header{
		Type: packfile.Blob,
		Size: 1,
	})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, blobContent); err != nil {
		return err
	}
	return nil
}

func tooShort() (err error) {
	// You will need to comment out integrity checks in the writer to have this run.
	w := packfile.NewWriter(os.Stdout, 1)
	defer func() {
		if closeErr := w.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	const blobContent = "Hello"
	_, err = w.WriteHeader(&packfile.Header{
		Type: packfile.Blob,
		Size: int64(len(blobContent) + 1),
	})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, blobContent); err != nil {
		return err
	}
	return nil
}

func hashObject(typ object.Type, data []byte) [sha1.Size]byte {
	buf := object.AppendPrefix(nil, typ, int64(len(data)))
	buf = append(buf, data...)
	return sha1.Sum(buf)
}
