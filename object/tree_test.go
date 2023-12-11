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
	"encoding"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"gg-scm.io/pkg/git/githash"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var (
	_ encoding.BinaryUnmarshaler = new(Tree)
	_ encoding.BinaryMarshaler   = Tree(nil)
)

var treeTests = []struct {
	name   string
	id     githash.SHA1
	parsed Tree
}{
	{
		name:   "Empty",
		id:     hashLiteral("4b825dc642cb6eb9a060e54bf8d69288fbee4904"),
		parsed: Tree{},
	},
	{
		name: "SingleFile",
		id:   hashLiteral("a47995a165e75bf2a04b3d4165ff850449dbc542"),
		parsed: Tree{
			{
				Name:     "settings.json",
				Mode:     0100644,
				ObjectID: hashLiteral("19571d8eb68230d997eb0254bdc50ab0c1085598"),
			},
		},
	},
	{
		name: "FlatList",
		id:   hashLiteral("58452ad47a5fd3119fb974f9af1818bc88f56857"),
		parsed: Tree{
			{
				Name:     ".gitignore",
				Mode:     0100644,
				ObjectID: hashLiteral("5f1b3cd904bfb0f7e28917897d2dcb659a1980dd"),
			},
			{
				Name:     "go.mod",
				Mode:     0100644,
				ObjectID: hashLiteral("3d6156efac8ac8e403bd3ab5edb7884c8d48faae"),
			},
			{
				Name:     "go.sum",
				Mode:     0100644,
				ObjectID: hashLiteral("e21464d5acf0fd836652889ab37a9afdbcbeb2ba"),
			},
			{
				Name:     "init.go",
				Mode:     0100644,
				ObjectID: hashLiteral("158a819902699f8359045450b48db869b3fd305d"),
			},
			{
				Name:     "main.go",
				Mode:     0100644,
				ObjectID: hashLiteral("0b8a78624bbba5e8f79f7bd459b51d2a4b03107d"),
			},
			{
				Name:     "schema.go",
				Mode:     0100644,
				ObjectID: hashLiteral("cc829be7395b4660f3dd360aea843b5423ba3ff4"),
			},
		},
	},
	{
		name: "Subdirectory",
		id:   hashLiteral("1ce1c00b1f6814f671085fc60aad44a719ce9422"),
		parsed: Tree{
			{
				Name:     ".gitignore",
				Mode:     0100644,
				ObjectID: hashLiteral("5f1b3cd904bfb0f7e28917897d2dcb659a1980dd"),
			},
			{
				Name:     ".vscode",
				Mode:     040000,
				ObjectID: hashLiteral("a47995a165e75bf2a04b3d4165ff850449dbc542"),
			},
			{
				Name:     "go.mod",
				Mode:     0100644,
				ObjectID: hashLiteral("3d6156efac8ac8e403bd3ab5edb7884c8d48faae"),
			},
			{
				Name:     "go.sum",
				Mode:     0100644,
				ObjectID: hashLiteral("e21464d5acf0fd836652889ab37a9afdbcbeb2ba"),
			},
			{
				Name:     "init.go",
				Mode:     0100644,
				ObjectID: hashLiteral("7ebb0d9d9434c08a5c357c3a7b1a8d0a47f17e66"),
			},
			{
				Name:     "main.go",
				Mode:     0100644,
				ObjectID: hashLiteral("efca101c3f7333df06b532e171f89501fb37c0b3"),
			},
			{
				Name:     "root.go",
				Mode:     0100644,
				ObjectID: hashLiteral("0e467721ee68dd86b98cd2f613c15a2fb953a275"),
			},
			{
				Name:     "schema.go",
				Mode:     0100644,
				ObjectID: hashLiteral("cc829be7395b4660f3dd360aea843b5423ba3ff4"),
			},
		},
	},
}

func TestParseTree(t *testing.T) {
	for _, test := range treeTests {
		t.Run(test.name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("testdata", fmt.Sprintf("tree-%x", test.id)))
			if err != nil {
				t.Fatal(err)
			}
			got, err := ParseTree(src)
			if err != nil {
				t.Error("Error:", err)
			}
			diff := cmp.Diff(test.parsed, got, cmpopts.EquateEmpty())
			if diff != "" {
				t.Errorf("tree (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTreeMarshalBinary(t *testing.T) {
	for _, test := range treeTests {
		t.Run(test.name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", fmt.Sprintf("tree-%x", test.id)))
			if err != nil {
				t.Fatal(err)
			}
			got, err := test.parsed.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("appendTo(nil) (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTreeSHA1(t *testing.T) {
	for _, test := range treeTests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.parsed.SHA1(); got != test.id {
				t.Errorf("sha1() = %x; want %x", got, test.id)
			}
		})
	}
}

func TestMode(t *testing.T) {
	tests := []struct {
		name       string
		mode       Mode
		isRegular  bool
		isDir      bool
		fileMode   fs.FileMode
		fileModeOK bool
		string     string
		octal      string
		hex        string
	}{
		{
			name:       "Zero",
			mode:       0,
			isRegular:  false,
			isDir:      false,
			fileMode:   0,
			fileModeOK: false,
			string:     "000000",
			octal:      "0",
			hex:        "0",
		},
		{
			name:       "Plain",
			mode:       ModePlain,
			isRegular:  true,
			isDir:      false,
			fileMode:   0o644,
			fileModeOK: true,
			string:     "100644",
			octal:      "100644",
			hex:        "81a4",
		},
		{
			name:       "PlainGroupWritable",
			mode:       ModePlainGroupWritable,
			isRegular:  true,
			isDir:      false,
			fileMode:   0o664,
			fileModeOK: true,
			string:     "100664",
			octal:      "100664",
			hex:        "81b4",
		},
		{
			name:       "Executable",
			mode:       ModeExecutable,
			isRegular:  true,
			isDir:      false,
			fileMode:   0o755,
			fileModeOK: true,
			string:     "100755",
			octal:      "100755",
			hex:        "81ed",
		},
		{
			name:       "Dir",
			mode:       ModeDir,
			isRegular:  false,
			isDir:      true,
			fileMode:   fs.ModeDir,
			fileModeOK: true,
			string:     "040000",
			octal:      "40000",
			hex:        "4000",
		},
		{
			name:       "Symlink",
			mode:       ModeSymlink,
			isRegular:  false,
			isDir:      false,
			fileMode:   fs.ModeSymlink,
			fileModeOK: true,
			string:     "120000",
			octal:      "120000",
			hex:        "a000",
		},
		{
			name:       "Gitlink",
			mode:       ModeGitlink,
			isRegular:  false,
			isDir:      false,
			fileMode:   fs.ModeDir | fs.ModeSymlink,
			fileModeOK: true,
			string:     "160000",
			octal:      "160000",
			hex:        "e000",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.mode.IsRegular(); got != test.isRegular {
				t.Errorf("IsRegular() = %t; want %t", got, test.isRegular)
			}
			if got := test.mode.IsDir(); got != test.isDir {
				t.Errorf("IsDir() = %t; want %t", got, test.isDir)
			}
			if got, ok := test.mode.FileMode(); got != test.fileMode || ok != test.fileModeOK {
				t.Errorf("FileMode() = %v, %t; want %v, %t", got, ok, test.fileMode, test.fileModeOK)
			}
			if got := test.mode.String(); got != test.string {
				t.Errorf("String() = %q; want %q", got, test.string)
			}
			if got := fmt.Sprintf("%s", test.mode); got != test.string {
				t.Errorf("fmt.Sprintf(\"%%s\") = %q; want %q", got, test.string)
			}
			if got := fmt.Sprintf("%v", test.mode); got != test.string {
				t.Errorf("fmt.Sprintf(\"%%v\") = %q; want %q", got, test.string)
			}
			if got := fmt.Sprintf("%o", test.mode); got != test.octal {
				t.Errorf("fmt.Sprintf(\"%%o\") = %q; want %q", got, test.octal)
			}
			if got := fmt.Sprintf("%x", test.mode); got != test.hex {
				t.Errorf("fmt.Sprintf(\"%%x\") = %q; want %q", got, test.hex)
			}
		})
	}
}
