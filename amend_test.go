// Copyright 2019 The gg Authors
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
	"path/filepath"
	"testing"
	"time"

	"gg-scm.io/pkg/git/internal/filesystem"
	"gg-scm.io/pkg/git/object"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestAmend(t *testing.T) {
	gitPath, err := findGit()
	if err != nil {
		t.Skip("git not found:", err)
	}
	ctx := context.Background()

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
		if err := env.g.Commit(ctx, "initial import", CommitOptions{}); err != nil {
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

		// Call g.Amend.
		const (
			wantAuthor    object.User = "Lisbeth Salander <lisbeth@example.com>"
			wantCommitter object.User = "Octo Cat <noreply@github.com>"
			wantMessage               = "\n\ninternal/git made this commit"
		)
		wantAuthorTime := time.Date(2018, time.February, 20, 15, 47, 42, 0, time.FixedZone("UTC-8", -8*60*60))
		wantCommitTime := time.Date(2018, time.December, 29, 8, 58, 24, 0, time.FixedZone("UTC-8", -8*60*60))
		err = env.g.Amend(ctx, AmendOptions{
			Message:    wantMessage,
			Author:     wantAuthor,
			AuthorTime: wantAuthorTime,
			Committer:  wantCommitter,
			CommitTime: wantCommitTime,
		})
		if err != nil {
			t.Error("Amend error:", err)
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
			Parents:    []Hash{},
			Author:     wantAuthor,
			AuthorTime: wantAuthorTime,
			Committer:  wantCommitter,
			CommitTime: wantCommitTime,
			Message:    wantMessage,
		}
		if diff := cmp.Diff(want, got, equateTruncatedTime(time.Second), cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("CommitInfo(ctx, \"HEAD\") diff (-want +got):\n%s", diff)
		}
		// Verify that HEAD is still pointing to main.
		if head, err := env.g.Head(ctx); err != nil {
			t.Error(err)
		} else if head.Ref != "refs/heads/main" {
			t.Errorf("HEAD ref = %s; want refs/heads/main", head.Ref)
		}
	})

	t.Run("DefaultOptions", func(t *testing.T) {
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
		const wantAuthor object.User = "Lisbeth Salander <lisbeth@example.com>"
		const wantMessage = "\n\ninternal/git made this commit"
		wantAuthorTime := time.Date(2018, time.February, 20, 15, 47, 42, 0, time.FixedZone("UTC-8", -8*60*60))
		err = env.g.Commit(ctx, wantMessage, CommitOptions{
			Author:     wantAuthor,
			AuthorTime: wantAuthorTime,
		})
		if err != nil {
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

		// Set committer in configuration.
		const wantCommitter object.User = "Octo Cat <noreply@github.com>"
		config := "[user]\nname = " + wantCommitter.Name() +
			"\nemail = " + wantCommitter.Email() + "\n"
		if err := env.top.Apply(filesystem.Write(".gitconfig", config)); err != nil {
			t.Fatal(err)
		}

		// Call g.Amend with empty options.
		err = env.g.Amend(ctx, AmendOptions{})
		if err != nil {
			t.Error("Amend error:", err)
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
			Parents:    []Hash{},
			Author:     wantAuthor,
			AuthorTime: wantAuthorTime,
			Committer:  wantCommitter,
			Message:    wantMessage,
		}
		if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(object.Commit{}, "CommitTime"), equateTruncatedTime(time.Second), cmpopts.EquateEmpty()); diff != "" {
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

func TestAmendFiles(t *testing.T) {
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
		if err := env.g.Init(ctx, "."); err != nil {
			t.Fatal(err)
		}

		// Create the parent commit.
		const (
			oldContent = "old content\n"
			newContent = "new content\n"
		)
		if err := env.root.Apply(filesystem.Write("file.txt", oldContent)); err != nil {
			t.Fatal(err)
		}
		if err := env.g.Add(ctx, []Pathspec{"file.txt"}, AddOptions{}); err != nil {
			t.Fatal(err)
		}
		if err := env.g.Commit(ctx, "initial import", CommitOptions{}); err != nil {
			t.Fatal(err)
		}

		// Change the content locally and stage it.
		if err := env.root.Apply(filesystem.Write("file.txt", newContent)); err != nil {
			t.Fatal(err)
		}
		if err := env.g.Add(ctx, []Pathspec{"file.txt"}, AddOptions{}); err != nil {
			t.Fatal(err)
		}

		// Call g.AmendFiles.
		const wantMessage = "\n\ninternal/git amended this commit\n\n"
		if err := env.g.AmendFiles(ctx, nil, AmendOptions{Message: wantMessage}); err != nil {
			t.Error("AmendFiles error:", err)
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

		// Verify that the file content was unchanged.
		if got, err := catFile(ctx, env.g, "HEAD", "file.txt"); err != nil {
			t.Error(err)
		} else if got != oldContent {
			t.Errorf("file.txt @ HEAD = %q; want %q", got, oldContent)
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

		// Call g.AmendFiles.
		const (
			wantAuthor    object.User = "Lisbeth Salander <lisbeth@example.com>"
			wantCommitter object.User = "Octo Cat <noreply@github.com>"
			wantMessage               = "\n\ninternal/git made this commit"
		)
		wantAuthorTime := time.Date(2018, time.February, 20, 15, 47, 42, 0, time.FixedZone("UTC-8", -8*60*60))
		wantCommitTime := time.Date(2018, time.December, 29, 8, 58, 24, 0, time.FixedZone("UTC-8", -8*60*60))
		err = env.g.AmendFiles(ctx, []Pathspec{"unstaged.txt"}, AmendOptions{
			Message:    wantMessage,
			Author:     wantAuthor,
			AuthorTime: wantAuthorTime,
			Committer:  wantCommitter,
			CommitTime: wantCommitTime,
		})
		if err != nil {
			t.Error("AmendFiles error:", err)
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
			Parents:    []Hash{},
			Author:     wantAuthor,
			AuthorTime: wantAuthorTime,
			Committer:  wantCommitter,
			CommitTime: wantCommitTime,
			Message:    wantMessage,
		}
		if diff := cmp.Diff(want, got, equateTruncatedTime(time.Second), cmpopts.EquateEmpty()); diff != "" {
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
			// Add an untouched file to keep directory and repository non-empty.
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
		const wantMessage = "initial import"
		if err := env.g.Run(ctx, "commit", "-m", wantMessage); err != nil {
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

		// Call g.AmendFiles from bar.
		amendPathspecs := []Pathspec{
			Pathspec(filepath.Join("..", "foo.txt")),
			"baz.txt",
		}
		err = env.g.WithDir("bar").AmendFiles(ctx, amendPathspecs, AmendOptions{})
		if err != nil {
			t.Error("AmendFiles:", err)
		}

		got, err := env.g.CommitInfo(ctx, "HEAD")
		if err != nil {
			t.Fatal(err)
		}
		// Verify that HEAD was moved to a new commit.
		if got.SHA1() == r1.Commit {
			t.Error("new HEAD = initial import")
		}
		// Verify that commit took parents of parent commit.
		if len(got.Parents) > 0 {
			t.Errorf("parents = %v; want []", got.Parents)
		}
		// Verify file contents of commit.
		wantTree := object.Tree{
			{
				Name: "bar",
				Mode: object.ModeDir,
				ObjectID: object.Tree{
					{
						Name:     "anchor.txt",
						Mode:     object.ModePlain,
						ObjectID: blobSum(dummyContent),
					},
				}.SHA1(),
			},
		}.SHA1()
		if got.Tree != wantTree {
			t.Errorf("tree = %v; want %v", got.Tree, wantTree)
		}
	})
}

func TestAmendAll(t *testing.T) {
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
	if err := env.g.Commit(ctx, "initial import", CommitOptions{}); err != nil {
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

	// Call g.AmendAll.
	const (
		wantAuthor    object.User = "Lisbeth Salander <lisbeth@example.com>"
		wantCommitter object.User = "Octo Cat <noreply@github.com>"
		wantMessage               = "\n\ninternal/git made this commit"
	)
	wantAuthorTime := time.Date(2018, time.February, 20, 15, 47, 42, 0, time.FixedZone("UTC-8", -8*60*60))
	wantCommitTime := time.Date(2018, time.December, 29, 8, 58, 24, 0, time.FixedZone("UTC-8", -8*60*60))
	err = env.g.AmendAll(ctx, AmendOptions{
		Message:    wantMessage,
		Author:     wantAuthor,
		AuthorTime: wantAuthorTime,
		Committer:  wantCommitter,
		CommitTime: wantCommitTime,
	})
	if err != nil {
		t.Error("AmendAll error:", err)
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
		Parents:    []Hash{},
		Author:     wantAuthor,
		AuthorTime: wantAuthorTime,
		Committer:  wantCommitter,
		CommitTime: wantCommitTime,
		Message:    wantMessage,
	}
	if diff := cmp.Diff(want, got, equateTruncatedTime(time.Second), cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("CommitInfo(ctx, \"HEAD\") diff (-want +got):\n%s", diff)
	}
	// Verify that HEAD is still pointing to main.
	if head, err := env.g.Head(ctx); err != nil {
		t.Error(err)
	} else if head.Ref != "refs/heads/main" {
		t.Errorf("HEAD ref = %s; want refs/heads/main", head.Ref)
	}
}
