// Copyright 2018 The gg Authors
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

package git

import (
	"context"
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gg-scm.io/pkg/git/internal/filesystem"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestListTree(t *testing.T) {
	gitPath, err := findGit()
	if err != nil {
		t.Skip("git not found:", err)
	}
	ctx := context.Background()
	env, err := newTestEnv(ctx, gitPath)
	if err != nil {
		t.Fatal(err)
	}
	defer env.cleanup()

	// Create a repository with one commit with only foo.txt and another commit
	// with both foo.txt and bar.txt. Uses raw commands, as ListTree is used to
	// verify the state of other APIs.
	if err := env.g.Init(ctx, "."); err != nil {
		t.Fatal(err)
	}
	err = env.root.Apply(filesystem.Write("foo.txt", dummyContent))
	if err != nil {
		t.Fatal(err)
	}
	foo := &TreeEntry{
		path:   "foo.txt",
		typ:    "blob",
		mode:   0644,
		object: gitObjectHash("blob", []byte(dummyContent)),
		size:   int64(len(dummyContent)),
	}
	if err := env.g.Run(ctx, "add", "foo.txt"); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Run(ctx, "commit", "-m", "commit 1"); err != nil {
		t.Fatal(err)
	}
	symlinkOp := filesystem.Symlink("foo.txt", "mylink")
	if runtime.GOOS == "windows" {
		symlinkOp = filesystem.Write("mylink", "")
	}
	err = env.root.Apply(
		filesystem.Write("bar/baz.txt", dummyContent),
		symlinkOp,
	)
	if err != nil {
		t.Fatal(err)
	}
	baz := &TreeEntry{
		path:   "bar/baz.txt",
		typ:    "blob",
		mode:   0644,
		object: gitObjectHash("blob", []byte(dummyContent)),
		size:   int64(len(dummyContent)),
	}
	bar := &TreeEntry{
		path: "bar",
		typ:  "tree",
		mode: os.ModeDir,
		object: gitObjectHash("tree",
			append(append([]byte(nil), "100644 baz.txt\x00"...), baz.object[:]...)),
	}
	mylink := &TreeEntry{
		path:   "mylink",
		typ:    "blob",
		mode:   os.ModeSymlink,
		object: gitObjectHash("blob", []byte("foo.txt")),
		size:   int64(len("foo.txt")),
	}
	if runtime.GOOS == "windows" {
		mylink = &TreeEntry{
			path:   "mylink",
			typ:    "blob",
			mode:   0644,
			object: gitObjectHash("blob", nil),
			size:   0,
		}
	}
	if err := env.g.Run(ctx, "add", filepath.Join("bar", "baz.txt"), "mylink"); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Run(ctx, "commit", "-m", "commit 2"); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Init(ctx, "submod"); err != nil {
		t.Fatal(err)
	}
	submodGit := env.g.WithDir("submod")
	if err := submodGit.Run(ctx, "commit", "--allow-empty", "-m", "first sub-commit"); err != nil {
		t.Fatal(err)
	}
	submodHead, err := submodGit.Head(ctx)
	submod := &TreeEntry{
		path:   "submod",
		typ:    "commit",
		mode:   os.ModeDir,
		object: submodHead.Commit,
	}
	if err := env.g.Run(ctx, "submodule", "add", "./submod", "submod"); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Run(ctx, "commit", "-m", "commit 3"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		dir  string
		rev  string
		opts ListTreeOptions
		want map[TopPath]*TreeEntry
	}{
		{
			name: "SingleFile",
			rev:  "HEAD~2",
			opts: ListTreeOptions{
				Recursive: true,
			},
			want: map[TopPath]*TreeEntry{
				"foo.txt": foo,
			},
		},
		{
			name: "MultipleFiles",
			rev:  "HEAD~",
			opts: ListTreeOptions{
				Recursive: true,
			},
			want: map[TopPath]*TreeEntry{
				"foo.txt":     foo,
				"bar/baz.txt": baz,
				"mylink":      mylink,
			},
		},
		{
			name: "Subdirectory",
			rev:  "HEAD~",
			opts: ListTreeOptions{
				Recursive: false,
			},
			want: map[TopPath]*TreeEntry{
				"foo.txt": foo,
				"bar":     bar,
				"mylink":  mylink,
			},
		},
		{
			name: "MultipleFiles/Filtered",
			rev:  "HEAD~",
			opts: ListTreeOptions{
				Recursive: true,
				Pathspecs: []Pathspec{"foo.txt"},
			},
			want: map[TopPath]*TreeEntry{
				"foo.txt": foo,
			},
		},
		{
			name: "Subdir/All",
			dir:  "bar",
			rev:  "HEAD~",
			opts: ListTreeOptions{
				Recursive: true,
			},
			want: map[TopPath]*TreeEntry{
				"foo.txt":     foo,
				"bar/baz.txt": baz,
				"mylink":      mylink,
			},
		},
		{
			name: "Subdir/Filter",
			dir:  "bar",
			rev:  "HEAD~",
			opts: ListTreeOptions{
				Recursive: true,
				Pathspecs: []Pathspec{LiteralPath(filepath.Join("..", "foo.txt"))},
			},
			want: map[TopPath]*TreeEntry{
				"foo.txt": foo,
			},
		},
		{
			name: "Submodules",
			rev:  "HEAD",
			opts: ListTreeOptions{
				Recursive: true,
				Pathspecs: []Pathspec{"submod"},
			},
			want: map[TopPath]*TreeEntry{
				"submod": submod,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := env.g
			if test.dir != "" {
				g = g.WithDir(env.root.FromSlash(test.dir))
			}
			got, err := g.ListTree(ctx, test.rev, test.opts)
			if err != nil {
				t.Fatal("ListTree error:", err)
			}
			diff := cmp.Diff(test.want, got,
				cmp.AllowUnexported(TreeEntry{}),
				cmpopts.EquateEmpty(),
			)
			if diff != "" {
				t.Errorf("ListTree (-want +got)\n%s", diff)
			}
		})
	}
	for _, test := range tests {
		t.Run("NameOnly/"+test.name, func(t *testing.T) {
			g := env.g
			if test.dir != "" {
				g = g.WithDir(env.root.FromSlash(test.dir))
			}
			opts := test.opts
			opts.NameOnly = true
			got, err := g.ListTree(ctx, test.rev, opts)
			if err != nil {
				t.Fatal("ListTree error:", err)
			}
			want := make(map[TopPath]*TreeEntry, len(test.want))
			for p := range test.want {
				want[p] = nil
			}
			diff := cmp.Diff(want, got,
				cmp.AllowUnexported(TreeEntry{}),
				cmpopts.EquateEmpty(),
			)
			if diff != "" {
				t.Errorf("ListTree (-want +got)\n%s", diff)
			}
		})
	}
}

// gitObjectHash hashes an object in the same way that Git will store it on
// disk. See https://git-scm.com/book/en/v2/Git-Internals-Git-Objects
func gitObjectHash(typ string, data []byte) Hash {
	s := sha1.New()
	fmt.Fprintf(s, "%s %d\x00", typ, len(data))
	s.Write(data)
	var h Hash
	copy(h[:], s.Sum(nil))
	return h
}
