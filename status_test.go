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
	"io"
	"testing"

	"gg-scm.io/pkg/git/internal/filesystem"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestReadStatusEntry(t *testing.T) {
	tests := []struct {
		name string
		data string
		mode int

		want      []StatusEntry
		remaining string
		err       func(error) bool
	}{
		{
			name: "Empty",
			data: "",
			err:  func(e error) bool { return e == io.EOF },
		},
		{
			name: "ModifiedWorkTree",
			data: " M foo.txt\x00",
			want: []StatusEntry{{
				Code: StatusCode{' ', 'M'},
				Name: "foo.txt",
			}},
		},
		{
			name: "MissingNul",
			data: " M foo.txt",
			err:  func(e error) bool { return e != nil && e != io.EOF },
		},
		{
			name: "ModifiedIndex",
			data: "MM foo.txt\x00",
			want: []StatusEntry{{
				Code: StatusCode{'M', 'M'},
				Name: "foo.txt",
			}},
		},
		{
			name: "Renamed",
			data: "R  bar.txt\x00foo.txt\x00",
			want: []StatusEntry{{
				Code: StatusCode{'R', ' '},
				Name: "bar.txt",
				From: "foo.txt",
			}},
		},
		{
			// Regression test for https://github.com/zombiezen/gg/issues/44
			name: "RenamedLocally",
			data: " R bar.txt\x00foo.txt\x00",
			mode: acceptRenames,
			want: []StatusEntry{{
				Code: StatusCode{' ', 'R'},
				Name: "bar.txt",
				From: "foo.txt",
			}},
		},
		{
			// Test for Git bug described in https://github.com/zombiezen/gg/issues/60
			name: "RenamedLocally_GoodInputWithGitBug",
			data: " R bar.txt\x00foo.txt\x00",
			mode: localRenameMissingName,
			want: []StatusEntry{{
				Code: StatusCode{' ', 'R'},
				Name: "",
				From: "bar.txt",
			}},
			remaining: "foo.txt\x00",
		},
		{
			// Test for Git bug described in https://github.com/zombiezen/gg/issues/60
			name: "RenamedLocally_GitBug",
			data: " R bar.txt\x00 A foo.txt\x00",
			mode: localRenameMissingName,
			want: []StatusEntry{{
				Code: StatusCode{' ', 'R'},
				Name: "",
				From: "bar.txt",
			}},
			remaining: " A foo.txt\x00",
		},
		{
			name: "Multiple",
			data: "R  bar.txt\x00foo.txt\x00MM baz.txt\x00",
			want: []StatusEntry{{
				Code: StatusCode{'R', ' '},
				Name: "bar.txt",
				From: "foo.txt",
			}},
			remaining: "MM baz.txt\x00",
		},
		{
			name: "DisabledRenames",
			data: " R bar.txt\x00foo.txt\x00",
			mode: rewriteLocalRenames,
			want: []StatusEntry{
				{
					Code: StatusCode{' ', 'A'},
					Name: "bar.txt",
				},
				{
					Code: StatusCode{' ', 'D'},
					Name: "foo.txt",
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, remaining, err := readStatusEntry(nil, test.data, test.mode)
			if err == nil {
				if test.err != nil {
					t.Fatalf("readStatusEntry(%q, %d) = %+v, %q, <nil>; want %+v, %q, <non-nil>", test.data, test.mode, got, remaining, test.want, test.remaining)
				}
				if !cmp.Equal(got, test.want, cmpopts.EquateEmpty()) || remaining != test.remaining {
					t.Fatalf("readStatusEntry(%q, %d) = %+v, %q, <nil>; want %+v, %q, <nil>", test.data, test.mode, got, remaining, test.want, test.remaining)
				}
			} else {
				if test.err == nil {
					t.Fatalf("readStatusEntry(%q, %d) = _, %q, %v; want %+v, %q, <nil>", test.data, test.mode, remaining, err, test.want, test.remaining)
				}
				if remaining != test.remaining || !test.err(err) {
					t.Fatalf("readStatusEntry(%q, %d) = _, %q, %v; want _, %q, <non-nil>", test.data, test.mode, remaining, err, test.remaining)
				}
			}
		})
	}
}

// Regression test for https://github.com/gg-scm/gg-git/issues/3
func TestDisableRenamesStatus(t *testing.T) {
	gitPath, err := findGit()
	if err != nil {
		t.Skip("git not found:", err)
	}
	ctx := context.Background()
	env, err := newTestEnv(ctx, gitPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(env.cleanup)
	if err := env.g.Init(ctx, "."); err != nil {
		t.Fatal(err)
	}
	err = env.root.Apply(
		filesystem.Write("foo.txt", dummyContent),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.g.Add(ctx, []Pathspec{"."}, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Commit(ctx, "hello world", CommitOptions{}); err != nil {
		t.Fatal(err)
	}
	err = env.root.Apply(
		filesystem.Remove("foo.txt"),
		filesystem.Write("bar.txt", dummyContent),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.g.Add(ctx, []Pathspec{"bar.txt"}, AddOptions{IntentToAdd: true}); err != nil {
		t.Fatal(err)
	}

	st, err := env.g.Status(ctx, StatusOptions{
		DisableRenames: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []StatusEntry{
		{Code: StatusCode{' ', 'D'}, Name: "foo.txt"},
		{Code: StatusCode{' ', 'A'}, Name: "bar.txt"},
	}
	diff := cmp.Diff(want, st,
		cmpopts.SortSlices(func(ent1, ent2 StatusEntry) bool {
			return ent1.Name < ent2.Name
		}),
		// Force String-ification in diff output.
		cmp.Comparer(func(s1, s2 StatusCode) bool {
			return s1 == s2
		}),
	)
	if diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestReadDiffStatusEntry(t *testing.T) {
	tests := []struct {
		name string
		data string

		want      DiffStatusEntry
		remaining string
		err       func(error) bool
	}{
		{
			name: "Empty",
			data: "",
			err:  func(e error) bool { return e == io.EOF },
		},
		{
			name: "Modified",
			data: "M\x00foo.txt\x00",
			want: DiffStatusEntry{
				Code: 'M',
				Name: "foo.txt",
			},
		},
		{
			name: "MissingNul",
			data: "M\x00foo.txt",
			err:  func(e error) bool { return e != nil && e != io.EOF },
		},
		{
			name: "Renamed",
			data: "R100\x00foo.txt\x00bar.txt\x00",
			want: DiffStatusEntry{
				Code: 'R',
				Name: "bar.txt",
			},
		},
		{
			name: "RenamedPartial",
			data: "R045\x00foo.txt\x00bar.txt\x00",
			want: DiffStatusEntry{
				Code: 'R',
				Name: "bar.txt",
			},
		},
		{
			name:      "RenamedScoreTooLong",
			data:      "R0000\x00foo.txt\x00bar.txt\x00",
			err:       func(e error) bool { return e != nil && e != io.EOF },
			remaining: "R0000\x00foo.txt\x00bar.txt\x00",
		},
		{
			name: "Multiple",
			data: "A\x00foo.txt\x00D\x00bar.txt\x00",
			want: DiffStatusEntry{
				Code: 'A',
				Name: "foo.txt",
			},
			remaining: "D\x00bar.txt\x00",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, remaining, err := readDiffStatusEntry(test.data)
			if err == nil {
				if test.err != nil {
					t.Fatalf("readDiffStatusEntry(%q) = %+v, %q, <nil>; want %+v, %q, <non-nil>", test.data, got, remaining, test.want, test.remaining)
				}
				if got != test.want || remaining != test.remaining {
					t.Fatalf("readDiffStatusEntry(%q) = %+v, %q, <nil>; want %+v, %q, <nil>", test.data, got, remaining, test.want, test.remaining)
				}
			} else {
				if test.err == nil {
					t.Fatalf("readDiffStatusEntry(%q) = _, %q, %v; want %+v, %q, <nil>", test.data, remaining, err, test.want, test.remaining)
				}
				if remaining != test.remaining || !test.err(err) {
					t.Fatalf("readDiffStatusEntry(%q) = _, %q, %v; want _, %q, <non-nil>", test.data, remaining, err, test.remaining)
				}
			}
		})
	}
}

func TestAffectedByStatusRenameBug(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"", false},
		{"git version 2.10", false},
		{"git version 2.10.0", false},
		{"git version 2.10.0.foobarbaz", false},
		{"git version 2.11", true},
		{"git version 2.11.0", true},
		{"git version 2.11.0.foobarbaz", true},
		{"git version 2.12", true},
		{"git version 2.12.0", true},
		{"git version 2.12.0.foobarbaz", true},
		{"git version 2.13", true},
		{"git version 2.13.0", true},
		{"git version 2.13.0.foobarbaz", true},
		{"git version 2.14", true},
		{"git version 2.14.0", true},
		{"git version 2.14.0.foobarbaz", true},
		{"git version 2.15", true},
		{"git version 2.15.0", true},
		{"git version 2.15.0.foobarbaz", true},
		{"git version 2.16", false},
		{"git version 2.16.0", false},
		{"git version 2.16.0.foobarbaz", false},
	}
	for _, test := range tests {
		if got := affectedByStatusRenameBug(test.version); got != test.want {
			t.Errorf("affectedByStatusRenameBug(%q) = %t; want %t", test.version, got, test.want)
		}
	}
}

func TestListSubmodules(t *testing.T) {
	tests := []struct {
		name        string
		modulesFile string
		want        map[string]*SubmoduleConfig
	}{
		{
			name: "EmptyFile",
		},
		{
			name: "SingleSubmodule",
			modulesFile: "[submodule \"foo/bar\"]\n" +
				"path = foo/bar\n" +
				"url = https://github.com/zombiezen/gg-git.git\n",
			want: map[string]*SubmoduleConfig{
				"foo/bar": {
					Path: "foo/bar",
					URL:  "https://github.com/zombiezen/gg-git.git",
				},
			},
		},
		{
			name: "MultipleSubmodules",
			modulesFile: "[submodule \"foo/bar\"]\n" +
				"path = foo/bar\n" +
				"url = https://github.com/zombiezen/gg-git.git\n" +
				"[submodule \"quux\"]\n" +
				"path = quux\n" +
				"url = https://example.com/quux.git\n",
			want: map[string]*SubmoduleConfig{
				"foo/bar": {
					Path: "foo/bar",
					URL:  "https://github.com/zombiezen/gg-git.git",
				},
				"quux": {
					Path: "quux",
					URL:  "https://example.com/quux.git",
				},
			},
		},
	}

	gitPath, err := findGit()
	if err != nil {
		t.Skip("git not found:", err)
	}
	ctx := context.Background()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env, err := newTestEnv(ctx, gitPath)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(env.cleanup)
			if err := env.g.Init(ctx, "."); err != nil {
				t.Fatal(err)
			}
			err = env.root.Apply(filesystem.Write(".gitmodules", test.modulesFile))
			if err != nil {
				t.Fatal(err)
			}

			got, err := env.g.ListSubmodules(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(test.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("submodules (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("NoFile", func(t *testing.T) {
		env, err := newTestEnv(ctx, gitPath)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(env.cleanup)
		if err := env.g.Init(ctx, "."); err != nil {
			t.Fatal(err)
		}

		got, err := env.g.ListSubmodules(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var want map[string]*SubmoduleConfig
		if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("submodules (-want +got):\n%s", diff)
		}
	})
}
