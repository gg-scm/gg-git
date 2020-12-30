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

package object

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"gg-scm.io/pkg/git/githash"
)

// A Tree is a Git tree object: a flat list of files in a directory.
// The entries must be sorted by name and contain no duplicates.
// The zero value is an empty tree.
type Tree []*TreeEntry

// ParseTree deserializes a tree in the Git object format. It is the same as
// calling UnmarshalBinary on a new tree.
func ParseTree(src []byte) (Tree, error) {
	var tree Tree
	err := tree.UnmarshalBinary(src)
	return tree, err
}

// MarshalBinary serializes the tree into the Git tree object format. It returns
// an error if the tree is not sorted or contains duplicates.
func (tree Tree) MarshalBinary() ([]byte, error) {
	var dst []byte
	for i, ent := range tree {
		if i > 0 && !tree.Less(i-1, i) {
			return nil, fmt.Errorf("marshal git tree: not sorted")
		}
		dst = ent.appendTo(dst)
	}
	return dst, nil
}

// UnmarshalBinary deserializes a tree from the Git object format.
// If UnmarshalBinary does not return an error, the tree will always be sorted.
func (tree *Tree) UnmarshalBinary(src []byte) error {
	*tree = nil
	for len(src) > 0 {
		var ent *TreeEntry
		var err error
		ent, src, err = parseTreeEntry(src)
		if err != nil {
			return fmt.Errorf("parse git tree: %w", err)
		}
		*tree = append(*tree, ent)
		if len(*tree) > 1 && !tree.Less(len(*tree)-2, len(*tree)-1) {
			return fmt.Errorf("parse git tree: not sorted")
		}
	}
	return nil
}

// String formats the tree in an ASCII-clean debugging format.
func (tree Tree) String() string {
	sb := new(strings.Builder)
	for i, ent := range tree {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(ent.String())
	}
	return sb.String()
}

// SHA1 computes the SHA-1 hash of the tree object. It panics if the tree is
// not sorted or contains duplicates.
func (tree Tree) SHA1() githash.SHA1 {
	buf, err := tree.MarshalBinary()
	if err != nil {
		panic(err)
	}
	h := sha1.New()
	h.Write(AppendPrefix(nil, "tree", int64(len(buf))))
	h.Write(buf)
	var arr githash.SHA1
	h.Sum(arr[:0])
	return arr
}

// Search returns the entry with the given name in the tree or nil if not found.
// It may return incorrect results if the tree is not sorted.
func (tree Tree) Search(name string) *TreeEntry {
	i := sort.Search(len(tree), func(i int) bool {
		return tree[i].Name >= name
	})
	if i >= len(tree) || tree[i].Name != name {
		return nil
	}
	return tree[i]
}

// Len returns the number of entries in the tree.
func (tree Tree) Len() int {
	return len(tree)
}

// Less reports whether the i'th entry name is less than the j'th entry name.
func (tree Tree) Less(i, j int) bool {
	return tree[i].Name < tree[j].Name
}

// Swap swaps the i'th entry with the j'th entry.
func (tree Tree) Swap(i, j int) {
	tree[i], tree[j] = tree[j], tree[i]
}

// Sort sorts the tree, returning an error if there are any duplicates.
func (tree Tree) Sort() error {
	sort.Sort(tree)
	for i := range tree {
		if i == 0 {
			continue
		}
		if name := tree[i].Name; tree[i-1].Name == name {
			return fmt.Errorf("sort git tree: found duplicate %q", name)
		}
	}
	return nil
}

// A TreeEntry represents a single file in a Git tree object.
type TreeEntry struct {
	Name     string
	Mode     Mode
	ObjectID githash.SHA1
}

func parseTreeEntry(src []byte) (_ *TreeEntry, tail []byte, _ error) {
	modeEnd := bytes.IndexByte(src, ' ')
	if modeEnd == -1 {
		return nil, src, fmt.Errorf("entry: mode: %w", io.ErrUnexpectedEOF)
	}
	mode, err := strconv.ParseUint(string(src[:modeEnd]), 8, 32)
	if err != nil {
		return nil, src, fmt.Errorf("entry: mode: %w", err)
	}
	ent := &TreeEntry{Mode: Mode(mode)}

	nameStart := modeEnd + 1
	nameEnd := bytes.IndexByte(src[nameStart:], 0)
	if nameEnd == -1 {
		return nil, src, fmt.Errorf("entry: name: %w", io.ErrUnexpectedEOF)
	}
	nameEnd += nameStart
	ent.Name = string(src[nameStart:nameEnd])

	hashStart := nameEnd + 1
	hashEnd := hashStart + len(ent.ObjectID)
	if hashEnd > len(src) {
		return nil, src, fmt.Errorf("entry: object ID: %w", io.ErrUnexpectedEOF)
	}
	copy(ent.ObjectID[:], src[hashStart:hashEnd])
	return ent, src[hashEnd:], nil
}

// appendTo formats the entry in the manner Git expects.
func (ent *TreeEntry) appendTo(dst []byte) []byte {
	dst = strconv.AppendUint(dst, uint64(ent.Mode), 8)
	dst = append(dst, ' ')
	dst = append(dst, ent.Name...)
	dst = append(dst, 0)
	dst = append(dst, ent.ObjectID[:]...)
	return dst
}

// String formats the entry in an ASCII-clean format similar to the Git tree
// object format.
func (ent *TreeEntry) String() string {
	sb := new(strings.Builder)
	sb.WriteString(ent.Mode.String())
	sb.WriteByte(' ')
	sb.WriteString(ent.Name)
	sb.WriteByte(' ')
	sb.Write(appendHex(nil, ent.ObjectID[:]))
	return sb.String()
}

// Mode references:
// https://stackoverflow.com/a/8347325
// https://github.com/git/git/blob/0ef60afdd4416345b16b5c4d8d0558a08d680bc5/compat/vcbuild/include/unistd.h#L71-L96
// https://en.wikibooks.org/wiki/C_Programming/POSIX_Reference/sys/stat.h

// Mode is a tree entry file mode. It is similar to os.FileMode, but is limited
// to a specific set of modes.
type Mode uint32

// Git tree entry modes.
const (
	// ModePlain indicates a non-executable file.
	ModePlain Mode = 0o100644
	// ModeExecutable indicates an executable file.
	ModeExecutable Mode = 0o100755
	// ModeDir indicates a subdirectory.
	ModeDir Mode = 0o040000
	// ModeSymlink indicates a symbolic link.
	ModeSymlink Mode = 0o120000
	// ModeGitlink indicates a Git submodule.
	ModeGitlink Mode = 0o160000

	// ModePlainGroupWritable indicates a non-executable file.
	// This is equivalent to ModePlain, but was sometimes generated by
	// older versions of Git.
	ModePlainGroupWritable Mode = 0o100664
)

// Mode bits
const (
	typeMask    Mode = 0o170000 // S_IFMT
	regularFile Mode = 0o100000 // S_IFREG
)

// IsRegular reports whether m describes a file.
func (m Mode) IsRegular() bool {
	return m&typeMask == regularFile
}

// IsDir reports whether m describes a directory.
func (m Mode) IsDir() bool {
	return m&typeMask == ModeDir
}

// String formats the mode as zero-padded octal.
func (m Mode) String() string {
	return fmt.Sprintf("%06o", uint32(m))
}

// Format implements fmt.Formatter to make %x and %X format the number rather
// than the string.
func (m Mode) Format(f fmt.State, c rune) {
	if c == 'v' && f.Flag('#') {
		fmt.Fprintf(f, "object.Mode(%O)", uint32(m))
		return
	}

	format := new(strings.Builder)
	format.WriteString("%")
	for _, flag := range "+-# 0" {
		if f.Flag(int(flag)) {
			format.WriteRune(flag)
		}
	}
	if width, ok := f.Width(); ok {
		format.Write(strconv.AppendInt(nil, int64(width), 10))
	}
	if prec, ok := f.Precision(); ok {
		format.WriteString(".")
		format.Write(strconv.AppendInt(nil, int64(prec), 10))
	}
	format.WriteRune(c)
	switch c {
	case 's', 'q', 'v':
		fmt.Fprintf(f, format.String(), m.String())
	default:
		fmt.Fprintf(f, format.String(), uint32(m))
	}
}

// FileMode converts the Git mode into an os.FileMode, if possible.
func (m Mode) FileMode() (f os.FileMode, ok bool) {
	perm := os.FileMode(m & 0o000777)
	switch m & typeMask {
	case regularFile:
		return perm, true
	case ModeDir:
		return os.ModeDir | perm, true
	case ModeSymlink:
		return os.ModeSymlink | perm, true
	default:
		return 0, false
	}
}
