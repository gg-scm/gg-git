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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gg-scm.io/pkg/git/internal/filesystem"
	"gg-scm.io/pkg/git/object"
	"github.com/google/go-cmp/cmp"
)

func TestCommit(t *testing.T) {
	gitPath, err := findGit()
	if err != nil {
		t.Skip("git not found:", err)
	}
	ctx := context.Background()

	t.Run("Message", func(t *testing.T) {
		tests := []struct {
			name    string
			msg     string
			wantErr bool
		}{
			{name: "Empty", msg: "", wantErr: true},
			{name: "OneLineNoEOL", msg: "hello world"},
			{name: "OneLine", msg: "hello world\n"},
			{name: "OneLineUntrimmed", msg: " \t hello world\t \t\n"},
			{name: "TwoLinesNoEOL", msg: "hello\nworld"},
			{name: "TwoLines", msg: "hello\nworld\n"},
			{name: "ThreeLinesNoEOL", msg: "hello\nworld\n!"},
			{name: "ThreeLines", msg: "hello\nworld\n!\n"},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				env, err := newTestEnv(ctx, gitPath)
				if err != nil {
					t.Fatal(err)
				}
				defer env.cleanup()
				if err := env.g.Init(ctx, "."); err != nil {
					t.Fatal(err)
				}
				if err := env.root.Apply(filesystem.Write("foo.txt", dummyContent)); err != nil {
					t.Fatal(err)
				}
				if err := env.g.Add(ctx, []Pathspec{"foo.txt"}, AddOptions{}); err != nil {
					t.Fatal(err)
				}
				if err := env.g.Commit(ctx, test.msg, CommitOptions{}); err != nil {
					if !test.wantErr {
						t.Error("Commit error:", err)
					}
					return
				}
				if test.wantErr {
					t.Fatal("Commit did not return error; want error")
				}
				info, err := env.g.CommitInfo(ctx, "HEAD")
				if err != nil {
					t.Fatal(err)
				}
				if info.Message != test.msg {
					t.Errorf("message = %q; want %q", info.Message, test.msg)
				}
			})
		}
	})

	t.Run("LocalChanges", func(t *testing.T) {
		env, err := newTestEnv(ctx, gitPath)
		if err != nil {
			t.Fatal(err)
		}
		defer env.cleanup()
		if err := env.g.Init(ctx, "."); err != nil {
			t.Fatal(err)
		}

		// Create the parent commit.
		const (
			addContent  = "And now...\n"
			modifiedOld = "The Larch\n"
			modifiedNew = "The Chestnut\n"
		)
		err = env.root.Apply(
			filesystem.Write("modified_unstaged.txt", modifiedOld),
			filesystem.Write("modified_staged.txt", modifiedOld),
			filesystem.Write("deleted.txt", dummyContent),
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := env.g.Add(ctx, []Pathspec{"modified_unstaged.txt", "modified_staged.txt", "deleted.txt"}, AddOptions{}); err != nil {
			t.Fatal(err)
		}
		// (Use command-line directly, so as not to depend on system-under-test.)
		if err := env.g.Run(ctx, "commit", "-m", "initial import"); err != nil {
			t.Fatal(err)
		}
		r1, err := env.g.Head(ctx)
		if err != nil {
			t.Fatal(err)
		}

		// Arrange working copy changes.
		err = env.root.Apply(
			filesystem.Write("modified_unstaged.txt", modifiedNew),
			filesystem.Write("modified_staged.txt", modifiedNew),
			filesystem.Write("added.txt", addContent),
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := env.g.Add(ctx, []Pathspec{"added.txt", "modified_staged.txt"}, AddOptions{}); err != nil {
			t.Fatal(err)
		}
		if err := env.g.Remove(ctx, []Pathspec{"deleted.txt"}, RemoveOptions{}); err != nil {
			t.Fatal(err)
		}

		// Call g.Commit.
		const (
			wantAuthor    object.User = "Lisbeth Salander <lisbeth@example.com>"
			wantCommitter object.User = "Octo Cat <noreply@github.com>"
			wantMessage               = "\n\ninternal/git made this commit"
		)
		wantAuthorTime := time.Date(2018, time.February, 20, 15, 47, 42, 0, time.FixedZone("UTC-8", -8*60*60))
		wantCommitTime := time.Date(2018, time.December, 29, 8, 58, 24, 0, time.FixedZone("UTC-8", -8*60*60))
		err = env.g.Commit(ctx, wantMessage, CommitOptions{
			Author:     wantAuthor,
			AuthorTime: wantAuthorTime,
			Committer:  wantCommitter,
			CommitTime: wantCommitTime,
		})
		if err != nil {
			t.Error("Commit error:", err)
		}

		got, err := env.g.CommitInfo(ctx, "HEAD")
		if err != nil {
			t.Fatal(err)
		}
		// Verify that HEAD was moved to a new commit.
		if got.SHA1() == r1.Commit {
			t.Error("new HEAD = initial import")
		}
		// Verify the commit fields.
		want := &object.Commit{
			Tree: object.Tree{
				{
					Name:     "added.txt",
					Mode:     object.ModePlain,
					ObjectID: blobSum(addContent),
				},
				{
					Name:     "modified_staged.txt",
					Mode:     object.ModePlain,
					ObjectID: blobSum(modifiedNew),
				},
				{
					Name:     "modified_unstaged.txt",
					Mode:     object.ModePlain,
					ObjectID: blobSum(modifiedOld),
				},
			}.SHA1(),
			Parents:    []Hash{r1.Commit},
			Author:     wantAuthor,
			AuthorTime: wantAuthorTime,
			Committer:  wantCommitter,
			CommitTime: wantCommitTime,
			Message:    wantMessage,
		}
		if diff := cmp.Diff(want, got, equateTruncatedTime(time.Second)); diff != "" {
			t.Errorf("CommitInfo(ctx, \"HEAD\") diff (-want +got):\n%s", diff)
		}
		// Verify that HEAD is still pointing to main.
		if head, err := env.g.Head(ctx); err != nil {
			t.Error(err)
		} else if head.Ref != "refs/heads/main" {
			t.Errorf("HEAD ref = %s; want refs/heads/main", head.Ref)
		}
	})
}

func TestCommitFiles(t *testing.T) {
	gitPath, err := findGit()
	if err != nil {
		t.Skip("git not found:", err)
	}
	ctx := context.Background()

	t.Run("Empty", func(t *testing.T) {
		env, err := newTestEnv(ctx, gitPath)
		if err != nil {
			t.Fatal(err)
		}
		defer env.cleanup()
		if version, err := env.g.getVersion(ctx); err == nil {
			// Versions of Git < 2.11.1 fail at creating empty commits.
			// Skip the test.
			skipPrefixes := []string{
				"git version 2.7",
				"git version 2.8",
				"git version 2.9",
				"git version 2.10",
				"git version 2.11",
			}
			for _, p := range skipPrefixes {
				if strings.HasPrefix(version, p) && (len(version) == len(p) || version[len(p)] == '.') {
					t.Skipf("Version = %q (<2.11.1); skipping", version)
				}
			}
			if strings.HasPrefix(version, "git version 2.11.0") {
				t.Skipf("Version = %q (<2.11.1); skipping", version)
			}
		}
		if err := env.g.Init(ctx, "."); err != nil {
			t.Fatal(err)
		}

		// Call g.CommitFiles.
		const wantMessage = "\n\ninternal/git made this commit\n\n"
		if err := env.g.CommitFiles(ctx, wantMessage, nil, CommitOptions{}); err != nil {
			t.Error("Commit error:", err)
		}

		// Verify that HEAD was moved to a new commit.
		r, err := env.g.Head(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if r.Ref != "refs/heads/main" {
			t.Errorf("HEAD ref = %s; want refs/heads/main", r.Ref)
		}

		// Verify commit message.
		info, err := env.g.CommitInfo(ctx, "HEAD")
		if err != nil {
			t.Fatal(err)
		}
		if info.Message != wantMessage {
			t.Errorf("message = %q; want %q", info.Message, wantMessage)
		}
	})

	t.Run("Unstaged", func(t *testing.T) {
		env, err := newTestEnv(ctx, gitPath)
		if err != nil {
			t.Fatal(err)
		}
		defer env.cleanup()
		if err := env.g.Init(ctx, "."); err != nil {
			t.Fatal(err)
		}

		// Create the parent commit.
		const (
			oldContent = "The Larch\n"
			newContent = "The Chestnut\n"
		)
		err = env.root.Apply(
			filesystem.Write("unstaged.txt", oldContent),
			filesystem.Write("staged.txt", oldContent),
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := env.g.Add(ctx, []Pathspec{"unstaged.txt", "staged.txt"}, AddOptions{}); err != nil {
			t.Fatal(err)
		}
		// (Use command-line directly, so as not to depend on system-under-test.)
		if err := env.g.Run(ctx, "commit", "-m", "initial import"); err != nil {
			t.Fatal(err)
		}
		r1, err := env.g.Head(ctx)
		if err != nil {
			t.Fatal(err)
		}

		// Arrange working copy changes.
		err = env.root.Apply(
			filesystem.Write("unstaged.txt", newContent),
			filesystem.Write("staged.txt", newContent),
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := env.g.Add(ctx, []Pathspec{"staged.txt"}, AddOptions{}); err != nil {
			t.Fatal(err)
		}

		// Call g.CommitFiles.
		const (
			wantAuthor    object.User = "Lisbeth Salander <lisbeth@example.com>"
			wantCommitter object.User = "Octo Cat <noreply@github.com>"
			wantMessage               = "\n\ninternal/git made this commit"
		)
		wantAuthorTime := time.Date(2018, time.February, 20, 15, 47, 42, 0, time.FixedZone("UTC-8", -8*60*60))
		wantCommitTime := time.Date(2018, time.December, 29, 8, 58, 24, 0, time.FixedZone("UTC-8", -8*60*60))
		err = env.g.CommitFiles(ctx, wantMessage, []Pathspec{"unstaged.txt"}, CommitOptions{
			Author:     wantAuthor,
			AuthorTime: wantAuthorTime,
			Committer:  wantCommitter,
			CommitTime: wantCommitTime,
		})
		if err != nil {
			t.Error("Commit error:", err)
		}

		got, err := env.g.CommitInfo(ctx, "HEAD")
		if err != nil {
			t.Fatal(err)
		}
		// Verify that HEAD was moved to a new commit.
		if got.SHA1() == r1.Commit {
			t.Error("new HEAD = initial import")
		}
		// Verify the commit fields.
		want := &object.Commit{
			Tree: object.Tree{
				{
					Name:     "staged.txt",
					Mode:     object.ModePlain,
					ObjectID: blobSum(oldContent),
				},
				{
					Name:     "unstaged.txt",
					Mode:     object.ModePlain,
					ObjectID: blobSum(newContent),
				},
			}.SHA1(),
			Parents:    []Hash{r1.Commit},
			Author:     wantAuthor,
			AuthorTime: wantAuthorTime,
			Committer:  wantCommitter,
			CommitTime: wantCommitTime,
			Message:    wantMessage,
		}
		if diff := cmp.Diff(want, got, equateTruncatedTime(time.Second)); diff != "" {
			t.Errorf("CommitInfo(ctx, \"HEAD\") diff (-want +got):\n%s", diff)
		}
		// Verify that HEAD is still pointing to main.
		if head, err := env.g.Head(ctx); err != nil {
			t.Error(err)
		} else if head.Ref != "refs/heads/main" {
			t.Errorf("HEAD ref = %s; want refs/heads/main", head.Ref)
		}
	})

	t.Run("FromSubdir", func(t *testing.T) {
		// Transparently handle the Git bug described in
		// https://github.com/zombiezen/gg/issues/10

		env, err := newTestEnv(ctx, gitPath)
		if err != nil {
			t.Fatal(err)
		}
		defer env.cleanup()

		// Create the parent commit.
		if err := env.g.Init(ctx, "."); err != nil {
			t.Fatal(err)
		}
		err = env.root.Apply(
			filesystem.Write("foo.txt", dummyContent),
			filesystem.Write("bar/baz.txt", dummyContent),
			// Add an untouched file to keep directory non-empty.
			filesystem.Write("bar/anchor.txt", dummyContent),
		)
		if err != nil {
			t.Fatal(err)
		}
		addPathspecs := []Pathspec{
			"foo.txt",
			Pathspec(filepath.Join("bar", "baz.txt")),
			Pathspec(filepath.Join("bar", "anchor.txt")),
		}
		if err := env.g.Add(ctx, addPathspecs, AddOptions{}); err != nil {
			t.Fatal(err)
		}
		// Use command-line directly, so as not to depend on system-under-test.
		if err := env.g.Run(ctx, "commit", "-m", "initial import"); err != nil {
			t.Fatal(err)
		}
		r1, err := env.g.Head(ctx)
		if err != nil {
			t.Fatal(err)
		}

		// Remove foo.txt and bar/baz.txt.
		rmPathspecs := []Pathspec{
			"foo.txt",
			Pathspec(filepath.Join("bar", "baz.txt")),
		}
		if err := env.g.Remove(ctx, rmPathspecs, RemoveOptions{}); err != nil {
			t.Fatal(err)
		}

		// Call g.CommitFiles from bar.
		commitPathspecs := []Pathspec{
			Pathspec(filepath.Join("..", "foo.txt")),
			"baz.txt",
		}
		err = env.g.WithDir("bar").CommitFiles(ctx, "my message", commitPathspecs, CommitOptions{})
		if err != nil {
			t.Error("CommitFiles:", err)
		}

		got, err := env.g.CommitInfo(ctx, "HEAD")
		if err != nil {
			t.Fatal(err)
		}
		// Verify that HEAD was moved to a new commit.
		if got.SHA1() == r1.Commit {
			t.Error("new HEAD = initial import")
		}
		// Verify file contents of commit.
		wantTree := map[TopPath]*TreeEntry{
			"bar/anchor.txt": nil,
		}
		tree, err := env.g.ListTree(ctx, "HEAD", ListTreeOptions{
			Recursive: true,
			NameOnly:  true,
		})
		if err != nil {
			t.Error(err)
		} else if diff := cmp.Diff(wantTree, tree, cmp.AllowUnexported(TreeEntry{})); diff != "" {
			t.Errorf("ListTree(ctx, \"HEAD\", nil) diff (-want +got):\n%s", diff)
		}
	})
}

func TestCommitAll(t *testing.T) {
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
	if err := env.g.Init(ctx, "."); err != nil {
		t.Fatal(err)
	}

	// Create the parent commit.
	const (
		oldContent = "The Larch\n"
		newContent = "The Chestnut\n"
	)
	err = env.root.Apply(
		filesystem.Write("unstaged.txt", oldContent),
		filesystem.Write("staged.txt", oldContent),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.g.Add(ctx, []Pathspec{"unstaged.txt", "staged.txt"}, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	// (Use command-line directly, so as not to depend on system-under-test.)
	if err := env.g.Run(ctx, "commit", "-m", "initial import"); err != nil {
		t.Fatal(err)
	}
	r1, err := env.g.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Arrange working copy changes.
	err = env.root.Apply(
		filesystem.Write("unstaged.txt", newContent),
		filesystem.Write("staged.txt", newContent),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.g.Add(ctx, []Pathspec{"staged.txt"}, AddOptions{}); err != nil {
		t.Fatal(err)
	}

	// Call g.CommitAll.
	const (
		wantAuthor    object.User = "Lisbeth Salander <lisbeth@example.com>"
		wantCommitter object.User = "Octo Cat <noreply@github.com>"
		wantMessage               = "\n\ninternal/git made this commit"
	)
	wantAuthorTime := time.Date(2018, time.February, 20, 15, 47, 42, 0, time.FixedZone("UTC-8", -8*60*60))
	wantCommitTime := time.Date(2018, time.December, 29, 8, 58, 24, 0, time.FixedZone("UTC-8", -8*60*60))
	err = env.g.CommitAll(ctx, wantMessage, CommitOptions{
		Author:     wantAuthor,
		AuthorTime: wantAuthorTime,
		Committer:  wantCommitter,
		CommitTime: wantCommitTime,
	})
	if err != nil {
		t.Error("Commit error:", err)
	}

	got, err := env.g.CommitInfo(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	// Verify that HEAD was moved to a new commit.
	if got.SHA1() == r1.Commit {
		t.Error("new HEAD = initial import")
	}
	// Verify the commit fields.
	want := &object.Commit{
		Tree: object.Tree{
			{
				Name:     "staged.txt",
				Mode:     object.ModePlain,
				ObjectID: blobSum(newContent),
			},
			{
				Name:     "unstaged.txt",
				Mode:     object.ModePlain,
				ObjectID: blobSum(newContent),
			},
		}.SHA1(),
		Parents:    []Hash{r1.Commit},
		Author:     wantAuthor,
		AuthorTime: wantAuthorTime,
		Committer:  wantCommitter,
		CommitTime: wantCommitTime,
		Message:    wantMessage,
	}
	if diff := cmp.Diff(want, got, equateTruncatedTime(time.Second)); diff != "" {
		t.Errorf("CommitInfo(ctx, \"HEAD\") diff (-want +got):\n%s", diff)
	}
	// Verify that HEAD is still pointing to main.
	if head, err := env.g.Head(ctx); err != nil {
		t.Error(err)
	} else if head.Ref != "refs/heads/main" {
		t.Errorf("HEAD ref = %s; want refs/heads/main", head.Ref)
	}
}

func catFile(ctx context.Context, g *Git, rev string, path TopPath) (string, error) {
	rc, err := g.Cat(ctx, rev, path)
	if err != nil {
		return "", err
	}
	sb := new(strings.Builder)
	if _, err := io.Copy(sb, rc); err != nil {
		rc.Close()
		return sb.String(), err
	}
	if err := rc.Close(); err != nil {
		return sb.String(), err
	}
	return sb.String(), nil
}
