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
	"strings"
	"testing"

	"gg-scm.io/pkg/git/internal/filesystem"
	"github.com/google/go-cmp/cmp"
)

func TestHeadRef(t *testing.T) {
	gitPath, err := findGit()
	if err != nil {
		t.Skip("git not found:", err)
	}
	ctx := context.Background()

	t.Run("EmptyRepo", func(t *testing.T) {
		env, err := newTestEnv(ctx, gitPath)
		if err != nil {
			t.Fatal(err)
		}
		defer env.cleanup()
		if err := env.g.Init(ctx, "."); err != nil {
			t.Fatal(err)
		}
		const want = Ref("refs/heads/main")
		got, err := env.g.HeadRef(ctx)
		if got != want || err != nil {
			t.Errorf("HeadRef(ctx) = %q, %v; want %q, <nil>", got, err, want)
		}
	})
	t.Run("FirstCommit", func(t *testing.T) {
		env, err := newTestEnv(ctx, gitPath)
		if err != nil {
			t.Fatal(err)
		}
		defer env.cleanup()
		if err := env.g.Init(ctx, "."); err != nil {
			t.Fatal(err)
		}
		if err := env.root.Apply(filesystem.Write("file.txt", dummyContent)); err != nil {
			t.Fatal(err)
		}
		if err := env.g.Run(ctx, "add", "file.txt"); err != nil {
			t.Fatal(err)
		}
		if err := env.g.Run(ctx, "commit", "-m", "first commit"); err != nil {
			t.Fatal(err)
		}
		const want = Ref("refs/heads/main")
		got, err := env.g.HeadRef(ctx)
		if got != want || err != nil {
			t.Errorf("HeadRef(ctx) = %q, %v; want %q, <nil>", got, err, want)
		}
	})
	t.Run("DetachedHead", func(t *testing.T) {
		env, err := newTestEnv(ctx, gitPath)
		if err != nil {
			t.Fatal(err)
		}
		defer env.cleanup()
		if err := env.g.Init(ctx, "."); err != nil {
			t.Fatal(err)
		}
		if err := env.root.Apply(filesystem.Write("file.txt", dummyContent)); err != nil {
			t.Fatal(err)
		}
		if err := env.g.Run(ctx, "add", "file.txt"); err != nil {
			t.Fatal(err)
		}
		if err := env.g.Run(ctx, "commit", "-m", "first commit"); err != nil {
			t.Fatal(err)
		}
		if err := env.g.Run(ctx, "checkout", "--quiet", "--detach", "HEAD"); err != nil {
			t.Fatal(err)
		}
		got, err := env.g.HeadRef(ctx)
		if got != "" || err != nil {
			t.Errorf("HeadRef(ctx) = %q, %v; want \"\", <nil>", got, err)
		}
	})
}

func TestParseRev(t *testing.T) {
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

	repoPath := env.root.FromSlash("repo")
	if err := env.g.Init(ctx, repoPath); err != nil {
		t.Fatal(err)
	}
	g := env.g.WithDir(repoPath)

	// First commit
	const fileName = "foo.txt"
	if err := env.root.Apply(filesystem.Write("repo/foo.txt", "Hello, World!\n")); err != nil {
		t.Fatal(err)
	}
	if err := g.Run(ctx, "add", fileName); err != nil {
		t.Fatal(err)
	}
	if err := g.Run(ctx, "commit", "-m", "first commit"); err != nil {
		t.Fatal(err)
	}
	commit1Hex, err := g.Output(ctx, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	commit1, err := ParseHash(strings.TrimSuffix(commit1Hex, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Run(ctx, "tag", "initial"); err != nil {
		t.Fatal(err)
	}
	if err := g.Run(ctx, "tag", "-a", "-m", "some notes", "initial_annotated"); err != nil {
		t.Fatal(err)
	}

	// Second commit
	if err := env.root.Apply(filesystem.Write("repo/foo.txt", "Some more thoughts...\n")); err != nil {
		t.Fatal(err)
	}
	if err := g.Run(ctx, "commit", "-a", "-m", "second commit"); err != nil {
		t.Fatal(err)
	}
	commit2Hex, err := g.Output(ctx, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	commit2, err := ParseHash(strings.TrimSuffix(commit2Hex, "\n"))
	if err != nil {
		t.Fatal(err)
	}

	// Run fetch (to write FETCH_HEAD)
	if err := g.Run(ctx, "fetch", repoPath, "HEAD"); err != nil {
		t.Fatal(err)
	}

	// Now verify:
	tests := []struct {
		refspec string
		commit  Hash
		ref     Ref
		err     bool
	}{
		{
			refspec: "",
			err:     true,
		},
		{
			refspec: "-",
			err:     true,
		},
		{
			refspec: "-HEAD",
			err:     true,
		},
		{
			refspec: "HEAD",
			commit:  commit2,
			ref:     "refs/heads/main",
		},
		{
			refspec: "FETCH_HEAD",
			commit:  commit2,
			ref:     "FETCH_HEAD",
		},
		{
			refspec: "main",
			commit:  commit2,
			ref:     "refs/heads/main",
		},
		{
			refspec: commit1.String(),
			commit:  commit1,
		},
		{
			refspec: commit2.String(),
			commit:  commit2,
		},
		{
			refspec: "initial",
			commit:  commit1,
			ref:     "refs/tags/initial",
		},
		{
			refspec: "initial_annotated",
			commit:  commit1,
			ref:     "refs/tags/initial_annotated",
		},
	}
	for _, test := range tests {
		rev, err := g.ParseRev(ctx, test.refspec)
		if err != nil {
			if !test.err {
				t.Errorf("ParseRev(ctx, g, %q) error: %v", test.refspec, err)
			}
			continue
		}
		if test.err {
			t.Errorf("ParseRev(ctx, g, %q) = %v; want error", test.refspec, rev)
			continue
		}
		if got := rev.Commit; got != test.commit {
			t.Errorf("ParseRev(ctx, g, %q).Commit() = %v; want %v", test.refspec, got, test.commit)
		}
		if got := rev.Ref; got != test.ref {
			t.Errorf("ParseRev(ctx, g, %q).RefName() = %q; want %q", test.refspec, got, test.ref)
		}
	}
}

func BenchmarkParseRev(b *testing.B) {
	gitPath, err := findGit()
	if err != nil {
		b.Skip("git not found:", err)
	}
	ctx := context.Background()
	env, err := newTestEnv(ctx, gitPath)
	if err != nil {
		b.Fatal(err)
	}
	defer env.cleanup()

	repoPath := env.root.FromSlash("repo")
	if err := env.g.Init(ctx, repoPath); err != nil {
		b.Fatal(err)
	}
	g := env.g.WithDir(repoPath)

	const fileName = "foo.txt"
	if err := env.root.Apply(filesystem.Write("repo/foo.txt", "Hello, World!\n")); err != nil {
		b.Fatal(err)
	}
	if err := g.Run(ctx, "add", fileName); err != nil {
		b.Fatal(err)
	}
	if err := g.Run(ctx, "commit", "-m", "first commit"); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := g.ParseRev(ctx, "HEAD"); err != nil {
			b.Fatal(err)
		}
	}
}

func TestListRefs(t *testing.T) {
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

	// Since ListRefs may be used to check state of other commands,
	// everything here uses raw commands.

	// Create the first main commit.
	if err := env.g.Init(ctx, "."); err != nil {
		t.Fatal(err)
	}
	if err := env.root.Apply(filesystem.Write("foo.txt", dummyContent)); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Run(ctx, "add", "foo.txt"); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Run(ctx, "commit", "-m", "first commit"); err != nil {
		t.Fatal(err)
	}
	revMain, err := env.g.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Create a new commit on branch abc.
	if err := env.g.Run(ctx, "checkout", "--quiet", "-b", "abc"); err != nil {
		t.Fatal(err)
	}
	if err := env.root.Apply(filesystem.Write("bar.txt", dummyContent)); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Run(ctx, "add", "bar.txt"); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Run(ctx, "commit", "-m", "abc commit"); err != nil {
		t.Fatal(err)
	}
	revABC, err := env.g.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Create a two new commits on branch def.
	if err := env.g.Run(ctx, "checkout", "--quiet", "-b", "def", "main"); err != nil {
		t.Fatal(err)
	}
	if err := env.root.Apply(filesystem.Write("baz.txt", dummyContent)); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Run(ctx, "add", "baz.txt"); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Run(ctx, "commit", "-m", "def commit 1"); err != nil {
		t.Fatal(err)
	}
	revDEF1, err := env.g.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.root.Apply(filesystem.Write("baz.txt", dummyContent+"abc\n")); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Run(ctx, "commit", "-a", "-m", "def commit 2"); err != nil {
		t.Fatal(err)
	}
	revDEF2, err := env.g.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Tag the def branch as "ghi".
	if err := env.g.Run(ctx, "tag", "-a", "-m", "tests gonna tag", "ghi", "HEAD~"); err != nil {
		t.Fatal(err)
	}

	// Call env.g.ListRefs().
	got, err := env.g.ListRefs(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Verify that refs match what we expect.
	want := map[Ref]Hash{
		"HEAD":            revDEF2.Commit,
		"refs/heads/main": revMain.Commit,
		"refs/heads/abc":  revABC.Commit,
		"refs/heads/def":  revDEF2.Commit,
		"refs/tags/ghi":   revDEF1.Commit,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("refs (-want +got):\n%s", diff)
	}
}

func TestListRefs_Empty(t *testing.T) {
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

	// Since ListRefs may be used to check state of other commands,
	// everything here uses raw commands.

	// Create the first main commit.
	if err := env.g.Init(ctx, "."); err != nil {
		t.Fatal(err)
	}

	// Call env.g.ListRefs().
	got, err := env.g.ListRefs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("refs = %v; want empty", got)
	}
}

func TestMutateRefs(t *testing.T) {
	gitPath, err := findGit()
	if err != nil {
		t.Skip("git not found:", err)
	}
	ctx := context.Background()

	setupRepo := func(ctx context.Context, env *testEnv) error {
		// Create the first main commit.
		if err := env.g.Init(ctx, "."); err != nil {
			return err
		}
		if err := env.root.Apply(filesystem.Write("foo.txt", dummyContent)); err != nil {
			return err
		}
		if err := env.g.Add(ctx, []Pathspec{"foo.txt"}, AddOptions{}); err != nil {
			return err
		}
		if err := env.g.Commit(ctx, "first commit", CommitOptions{}); err != nil {
			return err
		}

		// Create a new branch.
		if err := env.g.NewBranch(ctx, "foo", BranchOptions{}); err != nil {
			return err
		}

		return nil
	}

	t.Run("SetRef/NotExists", func(t *testing.T) {
		env, err := newTestEnv(ctx, gitPath)
		if err != nil {
			t.Fatal(err)
		}
		defer env.cleanup()
		if err := setupRepo(ctx, env); err != nil {
			t.Fatal(err)
		}
		foo, err := env.g.ParseRev(ctx, "refs/heads/foo")
		if err != nil {
			t.Fatal(err)
		}

		// Create the branch with MutateRefs.
		muts := map[Ref]RefMutation{"refs/heads/bar": SetRef(foo.Commit.String())}
		if err := env.g.MutateRefs(ctx, muts); err != nil {
			t.Errorf("MutateRefs(ctx, %v): %v", muts, err)
		}

		// Verify that "refs/heads/bar" points to the same object as "refs/heads/foo".
		if r, err := env.g.ParseRev(ctx, "refs/heads/bar"); err != nil {
			t.Error(err)
		} else if r.Commit != foo.Commit {
			t.Errorf("refs/heads/bar = %v; want %v", r.Commit, foo.Commit)
		}
	})

	t.Run("CreateRef/DoesNotExist", func(t *testing.T) {
		env, err := newTestEnv(ctx, gitPath)
		if err != nil {
			t.Fatal(err)
		}
		defer env.cleanup()
		if err := setupRepo(ctx, env); err != nil {
			t.Fatal(err)
		}
		foo, err := env.g.ParseRev(ctx, "refs/heads/foo")
		if err != nil {
			t.Fatal(err)
		}

		// Create the branch with MutateRefs.
		muts := map[Ref]RefMutation{"refs/heads/bar": CreateRef(foo.Commit.String())}
		if err := env.g.MutateRefs(ctx, muts); err != nil {
			t.Errorf("MutateRefs(ctx, %v): %v", muts, err)
		}

		// Verify that "refs/heads/bar" points to the same object as "refs/heads/foo".
		if r, err := env.g.ParseRev(ctx, "refs/heads/bar"); err != nil {
			t.Error(err)
		} else if r.Commit != foo.Commit {
			t.Errorf("refs/heads/bar = %v; want %v", r.Commit, foo.Commit)
		}
	})

	t.Run("CreatRef/Exists", func(t *testing.T) {
		env, err := newTestEnv(ctx, gitPath)
		if err != nil {
			t.Fatal(err)
		}
		defer env.cleanup()
		if err := setupRepo(ctx, env); err != nil {
			t.Fatal(err)
		}
		foo, err := env.g.ParseRev(ctx, "refs/heads/foo")
		if err != nil {
			t.Fatal(err)
		}

		// Attempt to create the branch with MutateRefs.
		muts := map[Ref]RefMutation{"refs/heads/foo": CreateRef(foo.Commit.String())}
		if err := env.g.MutateRefs(ctx, muts); err == nil {
			t.Errorf("MutateRefs(ctx, %v) did not return error", muts)
		}
	})

	t.Run("DeleteRef", func(t *testing.T) {
		env, err := newTestEnv(ctx, gitPath)
		if err != nil {
			t.Fatal(err)
		}
		defer env.cleanup()
		if err := setupRepo(ctx, env); err != nil {
			t.Fatal(err)
		}

		// Delete the branch with MutateRefs.
		muts := map[Ref]RefMutation{"refs/heads/foo": DeleteRef()}
		if err := env.g.MutateRefs(ctx, muts); err != nil {
			t.Errorf("MutateRefs(ctx, %v): %v", muts, err)
		}

		// Verify that "refs/heads/foo" is no longer valid.
		if r, err := env.g.ParseRev(ctx, "refs/heads/foo"); err == nil {
			t.Errorf("refs/heads/foo = %v; should not exist", r.Commit)
		}
	})

	t.Run("DeleteRef/DoesNotExist", func(t *testing.T) {
		env, err := newTestEnv(ctx, gitPath)
		if err != nil {
			t.Fatal(err)
		}
		defer env.cleanup()
		if err := setupRepo(ctx, env); err != nil {
			t.Fatal(err)
		}

		// Delete a non-existent branch "bar" with MutateRefs.
		muts := map[Ref]RefMutation{"refs/heads/bar": DeleteRef()}
		if err := env.g.MutateRefs(ctx, muts); err != nil {
			t.Errorf("MutateRefs(ctx, %v): %v", muts, err)
		}

		// Verify that "refs/heads/bar" still does not exist.
		if r, err := env.g.ParseRev(ctx, "refs/heads/bar"); err == nil {
			t.Errorf("refs/heads/bar = %v; should not exist", r.Commit)
		}
	})

	t.Run("DeleteRefIfMatches/Match", func(t *testing.T) {
		env, err := newTestEnv(ctx, gitPath)
		if err != nil {
			t.Fatal(err)
		}
		defer env.cleanup()
		if err := setupRepo(ctx, env); err != nil {
			t.Fatal(err)
		}
		r, err := env.g.Head(ctx)
		if err != nil {
			t.Fatal(err)
		}

		// Delete the branch with MutateRefs.
		muts := map[Ref]RefMutation{"refs/heads/foo": DeleteRefIfMatches(r.Commit.String())}
		if err := env.g.MutateRefs(ctx, muts); err != nil {
			t.Errorf("MutateRefs(ctx, %v): %v", muts, err)
		}

		// Verify that "refs/heads/foo" is no longer valid.
		if r, err := env.g.ParseRev(ctx, "refs/heads/foo"); err == nil {
			t.Errorf("refs/heads/foo = %v; should not exist", r.Commit)
		}
	})

	t.Run("DeleteRefIfMatches/NoMatch", func(t *testing.T) {
		env, err := newTestEnv(ctx, gitPath)
		if err != nil {
			t.Fatal(err)
		}
		defer env.cleanup()
		if err := setupRepo(ctx, env); err != nil {
			t.Fatal(err)
		}
		r, err := env.g.Head(ctx)
		if err != nil {
			t.Fatal(err)
		}

		// Attempt to delete the branch with MutateRefs.
		badCommit := r.Commit
		badCommit[len(badCommit)-1]++ // twiddle last byte
		muts := map[Ref]RefMutation{"refs/heads/foo": DeleteRefIfMatches(badCommit.String())}
		if err := env.g.MutateRefs(ctx, muts); err == nil {
			t.Errorf("MutateRefs(ctx, %v) did not return error", muts)
		}

		// Verify that "refs/heads/foo" has stayed the same.
		if got, err := env.g.ParseRev(ctx, "refs/heads/foo"); err != nil {
			t.Error(err)
		} else if got.Commit != r.Commit {
			t.Errorf("refs/heads/foo = %v; want %v", got.Commit, r.Commit)
		}
	})
}
