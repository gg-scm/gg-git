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
	"time"

	"gg-scm.io/pkg/git/internal/filesystem"
	"gg-scm.io/pkg/git/object"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestCommitInfo(t *testing.T) {
	gitPath, err := findGit()
	if err != nil {
		t.Skip("git not found:", err)
	}
	ctx := context.Background()

	// The commits created in these tests are entirely deterministic
	// because the dates and users are fixed, so their hashes will always
	// be the same.

	t.Run("EmptyMain", func(t *testing.T) {
		env, err := newTestEnv(ctx, gitPath)
		if err != nil {
			t.Fatal(err)
		}
		defer env.cleanup()

		if err := env.g.Init(ctx, "."); err != nil {
			t.Fatal(err)
		}
		_, err = env.g.CommitInfo(ctx, "main")
		if err == nil {
			t.Error("CommitInfo did not return error", err)
		}
	})

	t.Run("FirstCommit", func(t *testing.T) {
		env, err := newTestEnv(ctx, gitPath)
		if err != nil {
			t.Fatal(err)
		}
		defer env.cleanup()

		// Create a repository with a single commit to foo.txt.
		// Uses raw commands, as CommitInfo is used to verify the state of other APIs.
		if err := env.g.Init(ctx, "."); err != nil {
			t.Fatal(err)
		}
		if err := env.root.Apply(filesystem.Write("foo.txt", dummyContent)); err != nil {
			t.Fatal(err)
		}
		if err := env.g.Run(ctx, "add", "foo.txt"); err != nil {
			t.Fatal(err)
		}
		// Message does not have trailing newline to verify verbatim processing.
		const (
			wantAuthor    object.User = "Lisbeth Salander <lisbeth@example.com>"
			wantCommitter object.User = "Octo Cat <noreply@github.com>"
			wantMsg                   = "\t foobarbaz  \r\n\n  initial import  "
		)
		wantAuthorTime := time.Date(2018, time.February, 20, 15, 47, 42, 0, time.FixedZone("UTC-8", -8*60*60))
		wantCommitTime := time.Date(2018, time.December, 29, 8, 58, 24, 0, time.FixedZone("UTC-8", -8*60*60))
		err = env.g.Runner().RunGit(ctx, &Invocation{
			Args: []string{"commit", "--quiet", "--cleanup=verbatim", "--file=-"},
			Dir:  env.root.FromSlash("."),
			Env: []string{
				"GIT_AUTHOR_NAME=" + wantAuthor.Name(),
				"GIT_AUTHOR_EMAIL=" + wantAuthor.Email(),
				"GIT_AUTHOR_DATE=2018-02-20T15:47:42-08:00",
				"GIT_COMMITTER_NAME=" + wantCommitter.Name(),
				"GIT_COMMITTER_EMAIL=" + wantCommitter.Email(),
				"GIT_COMMITTER_DATE=2018-12-29T08:58:24-08:00",
			},
			Stdin: strings.NewReader(wantMsg),
		})
		if err != nil {
			t.Fatal(err)
		}
		got, err := env.g.CommitInfo(ctx, "HEAD")
		if err != nil {
			t.Fatal("CommitInfo:", err)
		}
		want := &object.Commit{
			Tree: object.Tree{
				{
					Name:     "foo.txt",
					Mode:     object.ModePlain,
					ObjectID: blobSum(dummyContent),
				},
			}.SHA1(),
			Author:     wantAuthor,
			AuthorTime: wantAuthorTime,
			Committer:  wantCommitter,
			CommitTime: wantCommitTime,
			Message:    wantMsg,
		}
		if diff := cmp.Diff(want, got, equateTruncatedTime(time.Second), cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("CommitInfo(ctx, \"HEAD\") diff (-want +got):\n%s", diff)
		}
	})

	t.Run("SecondCommit", func(t *testing.T) {
		env, err := newTestEnv(ctx, gitPath)
		if err != nil {
			t.Fatal(err)
		}
		defer env.cleanup()

		// Create a repository with two commits.
		// Uses raw commands, as CommitInfo is used to verify the state of other APIs.
		const (
			wantAuthor    object.User = "Lisbeth Salander <lisbeth@example.com>"
			wantCommitter object.User = "Octo Cat <noreply@github.com>"
		)
		if err := env.g.Init(ctx, "."); err != nil {
			t.Fatal(err)
		}
		if err := env.root.Apply(filesystem.Write("foo.txt", dummyContent)); err != nil {
			t.Fatal(err)
		}
		if err := env.g.Run(ctx, "add", "foo.txt"); err != nil {
			t.Fatal(err)
		}
		err = env.g.Runner().RunGit(ctx, &Invocation{
			Args: []string{"commit", "--quiet", "--cleanup=verbatim", "--file=-"},
			Dir:  env.root.FromSlash("."),
			Env: []string{
				"GIT_AUTHOR_NAME=" + wantAuthor.Name(),
				"GIT_AUTHOR_EMAIL=" + wantAuthor.Email(),
				"GIT_AUTHOR_DATE=2018-02-20T15:47:42-08:00",
				"GIT_COMMITTER_NAME=" + wantCommitter.Name(),
				"GIT_COMMITTER_EMAIL=" + wantCommitter.Email(),
				"GIT_COMMITTER_DATE=2018-12-29T08:58:24-08:00",
			},
			Stdin: strings.NewReader("initial import"),
		})
		if err != nil {
			t.Fatal(err)
		}
		commit0, err := env.g.CommitInfo(ctx, "HEAD")
		if err != nil {
			t.Fatal("CommitInfo:", err)
		}
		if err := env.g.Remove(ctx, []Pathspec{"foo.txt"}, RemoveOptions{}); err != nil {
			t.Fatal(err)
		}
		// Message does not have trailing newline to verify verbatim processing.
		const wantMsg = "\t foobarbaz  \r\n\n  the second commit  "
		wantAuthorTime := time.Date(2018, time.March, 21, 16, 26, 9, 0, time.FixedZone("UTC-7", -7*60*60))
		wantCommitTime := time.Date(2018, time.December, 29, 8, 58, 24, 0, time.FixedZone("UTC-8", -8*60*60))
		{
			err := env.g.Runner().RunGit(ctx, &Invocation{
				Args: []string{"commit", "--quiet", "--cleanup=verbatim", "--file=-"},
				Dir:  env.root.FromSlash("."),
				Env: []string{
					"GIT_AUTHOR_NAME=" + wantAuthor.Name(),
					"GIT_AUTHOR_EMAIL=" + wantAuthor.Email(),
					"GIT_AUTHOR_DATE=2018-03-21T16:26:09-07:00",
					"GIT_COMMITTER_NAME=" + wantCommitter.Name(),
					"GIT_COMMITTER_EMAIL=" + wantCommitter.Email(),
					"GIT_COMMITTER_DATE=2018-12-29T08:58:24-08:00",
				},
				Stdin: strings.NewReader(wantMsg),
			})
			if err != nil {
				t.Fatal(err)
			}
		}

		got, err := env.g.CommitInfo(ctx, "HEAD")
		if err != nil {
			t.Fatal("CommitInfo:", err)
		}
		want := &object.Commit{
			Tree:       object.Tree{}.SHA1(),
			Parents:    []Hash{commit0.SHA1()},
			Author:     wantAuthor,
			AuthorTime: wantAuthorTime,
			Committer:  wantCommitter,
			CommitTime: wantCommitTime,
			Message:    wantMsg,
		}
		if diff := cmp.Diff(want, got, equateTruncatedTime(time.Second), cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("CommitInfo(ctx, \"HEAD\") diff (-want +got):\n%s", diff)
		}
	})

	t.Run("MergeCommit", func(t *testing.T) {
		env, err := newTestEnv(ctx, gitPath)
		if err != nil {
			t.Fatal(err)
		}
		defer env.cleanup()

		// Create a repository with a merge commit.
		// Uses raw commands, as CommitInfo is used to verify the state of other APIs.
		const (
			wantAuthor    object.User = "Lisbeth Salander <lisbeth@example.com>"
			wantCommitter object.User = "Octo Cat <noreply@github.com>"
		)
		if err := env.g.Init(ctx, "."); err != nil {
			t.Fatal(err)
		}
		if err := env.root.Apply(filesystem.Write("foo.txt", dummyContent)); err != nil {
			t.Fatal(err)
		}
		if err := env.g.Run(ctx, "add", "foo.txt"); err != nil {
			t.Fatal(err)
		}
		err = env.g.Runner().RunGit(ctx, &Invocation{
			Args: []string{"commit", "--quiet", "--cleanup=verbatim", "--file=-"},
			Dir:  env.root.FromSlash("."),
			Env: []string{
				"GIT_AUTHOR_NAME=" + wantAuthor.Name(),
				"GIT_AUTHOR_EMAIL=" + wantAuthor.Email(),
				"GIT_AUTHOR_DATE=2018-02-20T15:47:42-08:00",
				"GIT_COMMITTER_NAME=" + wantCommitter.Name(),
				"GIT_COMMITTER_EMAIL=" + wantCommitter.Email(),
				"GIT_COMMITTER_DATE=2018-12-29T08:58:24-08:00",
			},
			Stdin: strings.NewReader("initial import"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := env.root.Apply(filesystem.Write("bar.txt", dummyContent)); err != nil {
			t.Fatal(err)
		}
		if err := env.g.Run(ctx, "add", "bar.txt"); err != nil {
			t.Fatal(err)
		}
		err = env.g.Runner().RunGit(ctx, &Invocation{
			Args: []string{"commit", "--quiet", "--cleanup=verbatim", "--file=-"},
			Dir:  env.root.FromSlash("."),
			Env: []string{
				"GIT_AUTHOR_NAME=" + wantAuthor.Name(),
				"GIT_AUTHOR_EMAIL=" + wantAuthor.Email(),
				"GIT_AUTHOR_DATE=2018-02-21T15:49:58-08:00",
				"GIT_COMMITTER_NAME=" + wantCommitter.Name(),
				"GIT_COMMITTER_EMAIL=" + wantCommitter.Email(),
				"GIT_COMMITTER_DATE=2018-12-29T08:58:24-08:00",
			},
			Stdin: strings.NewReader("first parent"),
		})
		if err != nil {
			t.Fatal(err)
		}
		parent0, err := env.g.CommitInfo(ctx, "HEAD")
		if err != nil {
			t.Fatal("CommitInfo:", err)
		}
		if err := env.g.Run(ctx, "checkout", "--quiet", "-b", "diverge", "HEAD~"); err != nil {
			t.Fatal(err)
		}
		if err := env.root.Apply(filesystem.Write("baz.txt", dummyContent)); err != nil {
			t.Fatal(err)
		}
		if err := env.g.Run(ctx, "add", "baz.txt"); err != nil {
			t.Fatal(err)
		}
		err = env.g.Runner().RunGit(ctx, &Invocation{
			Args: []string{"commit", "--quiet", "--cleanup=verbatim", "--file=-"},
			Dir:  env.root.FromSlash("."),
			Env: []string{
				"GIT_AUTHOR_NAME=" + wantAuthor.Name(),
				"GIT_AUTHOR_EMAIL=" + wantAuthor.Email(),
				"GIT_AUTHOR_DATE=2018-02-21T17:07:53-08:00",
				"GIT_COMMITTER_NAME=" + wantCommitter.Name(),
				"GIT_COMMITTER_EMAIL=" + wantCommitter.Email(),
				"GIT_COMMITTER_DATE=2018-12-29T08:58:24-08:00",
			},
			Stdin: strings.NewReader("second parent"),
		})
		if err != nil {
			t.Fatal(err)
		}
		parent1, err := env.g.CommitInfo(ctx, "HEAD")
		if err != nil {
			t.Fatal("CommitInfo:", err)
		}
		if err := env.g.Run(ctx, "checkout", "--quiet", "main"); err != nil {
			t.Fatal(err)
		}
		if err := env.g.Merge(ctx, []string{"diverge"}); err != nil {
			t.Fatal(err)
		}
		const wantMsg = "Merge branch 'diverge' into branch main\n"
		wantAuthorTime := time.Date(2018, time.February, 21, 19, 37, 26, 0, time.FixedZone("UTC-8", -8*60*60))
		wantCommitTime := time.Date(2018, time.December, 29, 8, 58, 24, 0, time.FixedZone("UTC-8", -8*60*60))
		err = env.g.Runner().RunGit(ctx, &Invocation{
			Args: []string{"commit", "--quiet", "--cleanup=verbatim", "--file=-"},
			Dir:  env.root.FromSlash("."),
			Env: []string{
				"GIT_AUTHOR_NAME=" + wantAuthor.Name(),
				"GIT_AUTHOR_EMAIL=" + wantAuthor.Email(),
				"GIT_AUTHOR_DATE=2018-02-21T19:37:26-08:00",
				"GIT_COMMITTER_NAME=" + wantCommitter.Name(),
				"GIT_COMMITTER_EMAIL=" + wantCommitter.Email(),
				"GIT_COMMITTER_DATE=2018-12-29T08:58:24-08:00",
			},
			Stdin: strings.NewReader(wantMsg),
		})
		if err != nil {
			t.Fatal(err)
		}

		got, err := env.g.CommitInfo(ctx, "HEAD")
		if err != nil {
			t.Fatal("CommitInfo:", err)
		}
		want := &object.Commit{
			Tree: object.Tree{
				{
					Name:     "bar.txt",
					Mode:     object.ModePlain,
					ObjectID: blobSum(dummyContent),
				},
				{
					Name:     "baz.txt",
					Mode:     object.ModePlain,
					ObjectID: blobSum(dummyContent),
				},
				{
					Name:     "foo.txt",
					Mode:     object.ModePlain,
					ObjectID: blobSum(dummyContent),
				},
			}.SHA1(),
			Parents:    []Hash{parent0.SHA1(), parent1.SHA1()},
			Author:     wantAuthor,
			AuthorTime: wantAuthorTime,
			Committer:  wantCommitter,
			CommitTime: wantCommitTime,
			Message:    wantMsg,
		}
		if diff := cmp.Diff(want, got, equateTruncatedTime(time.Second), cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("CommitInfo(ctx, \"HEAD\") diff (-want +got):\n%s", diff)
		}
	})
}

func TestLog(t *testing.T) {
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

	// Create a repository with a merge commit.
	//
	// The commits created in this test are entirely deterministic
	// because the dates and users are fixed, so their hashes will always
	// be the same.
	const (
		wantAuthor    object.User = "Lisbeth Salander <lisbeth@example.com>"
		wantCommitter object.User = "Octo Cat <noreply@github.com>"
	)
	commitOpts := func(t time.Time) CommitOptions {
		return CommitOptions{
			Author:     wantAuthor,
			AuthorTime: t,
			Committer:  wantCommitter,
			CommitTime: t,
		}
	}
	if err := env.g.Init(ctx, "."); err != nil {
		t.Fatal(err)
	}
	if err := env.root.Apply(filesystem.Write("foo.txt", dummyContent)); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Add(ctx, []Pathspec{"foo.txt"}, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	const wantMessage0 = "initial import\n"
	wantTime0 := time.Date(2018, time.February, 20, 15, 47, 42, 0, time.FixedZone("UTC-8", -8*60*60))
	if err := env.g.Commit(ctx, wantMessage0, commitOpts(wantTime0)); err != nil {
		t.Fatal(err)
	}

	if err := env.root.Apply(filesystem.Write("bar.txt", dummyContent)); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Add(ctx, []Pathspec{"bar.txt"}, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	const wantMessage1 = "first parent\nno trailing newline"
	wantTime1 := time.Date(2018, time.February, 21, 15, 49, 58, 0, time.FixedZone("UTC-8", -8*60*60))
	if err := env.g.Commit(ctx, wantMessage1, commitOpts(wantTime1)); err != nil {
		t.Fatal(err)
	}

	if err := env.g.NewBranch(ctx, "diverge", BranchOptions{Checkout: true, StartPoint: "HEAD~"}); err != nil {
		t.Fatal(err)
	}
	if err := env.root.Apply(filesystem.Write("baz.txt", dummyContent)); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Add(ctx, []Pathspec{"baz.txt"}, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	const wantMessage2 = "\t second parent\n"
	wantTime2 := time.Date(2018, time.February, 21, 17, 7, 53, 0, time.FixedZone("UTC-8", -8*60*60))
	if err := env.g.Commit(ctx, wantMessage2, commitOpts(wantTime2)); err != nil {
		t.Fatal(err)
	}
	if err := env.g.CheckoutBranch(ctx, "main", CheckoutOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Merge(ctx, []string{"diverge"}); err != nil {
		t.Fatal(err)
	}
	const wantMessage3 = "i merged\n"
	wantTime3 := time.Date(2018, time.February, 21, 19, 37, 26, 0, time.FixedZone("UTC-8", -8*60*60))
	if err := env.g.Commit(ctx, wantMessage3, commitOpts(wantTime3)); err != nil {
		t.Fatal(err)
	}

	log, err := env.g.Log(ctx, LogOptions{})
	if err != nil {
		t.Fatal("Log:", err)
	}
	var got []*object.Commit
	for log.Next() {
		got = append(got, log.CommitInfo())
	}
	if err := log.Close(); err != nil {
		t.Error("Close:", err)
	}
	commit0 := &object.Commit{
		Tree: object.Tree{
			{
				Name:     "foo.txt",
				Mode:     object.ModePlain,
				ObjectID: blobSum(dummyContent),
			},
		}.SHA1(),
		Author:     wantAuthor,
		Committer:  wantCommitter,
		AuthorTime: wantTime0,
		CommitTime: wantTime0,
		Message:    wantMessage0,
	}
	commit1 := &object.Commit{
		Tree: object.Tree{
			{
				Name:     "bar.txt",
				Mode:     object.ModePlain,
				ObjectID: blobSum(dummyContent),
			},
			{
				Name:     "foo.txt",
				Mode:     object.ModePlain,
				ObjectID: blobSum(dummyContent),
			},
		}.SHA1(),
		Parents:    []Hash{commit0.SHA1()},
		Author:     wantAuthor,
		Committer:  wantCommitter,
		AuthorTime: wantTime1,
		CommitTime: wantTime1,
		Message:    wantMessage1,
	}
	commit2 := &object.Commit{
		Tree: object.Tree{
			{
				Name:     "baz.txt",
				Mode:     object.ModePlain,
				ObjectID: blobSum(dummyContent),
			},
			{
				Name:     "foo.txt",
				Mode:     object.ModePlain,
				ObjectID: blobSum(dummyContent),
			},
		}.SHA1(),
		Parents:    []Hash{commit0.SHA1()},
		Author:     wantAuthor,
		Committer:  wantCommitter,
		AuthorTime: wantTime2,
		CommitTime: wantTime2,
		Message:    wantMessage2,
	}
	commit3 := &object.Commit{
		Tree: object.Tree{
			{
				Name:     "bar.txt",
				Mode:     object.ModePlain,
				ObjectID: blobSum(dummyContent),
			},
			{
				Name:     "baz.txt",
				Mode:     object.ModePlain,
				ObjectID: blobSum(dummyContent),
			},
			{
				Name:     "foo.txt",
				Mode:     object.ModePlain,
				ObjectID: blobSum(dummyContent),
			},
		}.SHA1(),
		Parents:    []Hash{commit1.SHA1(), commit2.SHA1()},
		Author:     wantAuthor,
		Committer:  wantCommitter,
		AuthorTime: wantTime3,
		CommitTime: wantTime3,
		Message:    wantMessage3,
	}
	want := []*object.Commit{
		commit3,
		commit2,
		commit1,
		commit0,
	}
	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("log diff (-want +got):\n%s", diff)
	}
}

func TestLog_NoWalk(t *testing.T) {
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

	// Create a repository with a merge commit.
	//
	// The commits created in this test are entirely deterministic
	// because the dates and users are fixed, so their hashes will always
	// be the same.
	const (
		wantAuthor    object.User = "Lisbeth Salander <lisbeth@example.com>"
		wantCommitter object.User = "Octo Cat <noreply@github.com>"
	)
	commitOpts := func(t time.Time) CommitOptions {
		return CommitOptions{
			Author:     wantAuthor,
			AuthorTime: t,
			Committer:  wantCommitter,
			CommitTime: t,
		}
	}
	if err := env.g.Init(ctx, "."); err != nil {
		t.Fatal(err)
	}
	if err := env.root.Apply(filesystem.Write("foo.txt", dummyContent)); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Add(ctx, []Pathspec{"foo.txt"}, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	const wantMessage0 = "initial import\n"
	wantTime0 := time.Date(2018, time.February, 20, 15, 47, 42, 0, time.FixedZone("UTC-8", -8*60*60))
	if err := env.g.Commit(ctx, wantMessage0, commitOpts(wantTime0)); err != nil {
		t.Fatal(err)
	}

	if err := env.root.Apply(filesystem.Write("bar.txt", dummyContent)); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Add(ctx, []Pathspec{"bar.txt"}, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	const wantMessage1 = "first parent\n"
	wantTime1 := time.Date(2018, time.February, 21, 15, 49, 58, 0, time.FixedZone("UTC-8", -8*60*60))
	if err := env.g.Commit(ctx, wantMessage1, commitOpts(wantTime1)); err != nil {
		t.Fatal(err)
	}

	log, err := env.g.Log(ctx, LogOptions{NoWalk: true})
	if err != nil {
		t.Fatal("Log:", err)
	}
	var got []*object.Commit
	for log.Next() {
		got = append(got, log.CommitInfo())
	}
	if err := log.Close(); err != nil {
		t.Error("Close:", err)
	}
	commit0 := &object.Commit{
		Tree: object.Tree{
			{
				Name:     "foo.txt",
				Mode:     object.ModePlain,
				ObjectID: blobSum(dummyContent),
			},
		}.SHA1(),
		Author:     wantAuthor,
		Committer:  wantCommitter,
		AuthorTime: wantTime0,
		CommitTime: wantTime0,
		Message:    wantMessage0,
	}
	commit1 := &object.Commit{
		Tree: object.Tree{
			{
				Name:     "bar.txt",
				Mode:     object.ModePlain,
				ObjectID: blobSum(dummyContent),
			},
			{
				Name:     "foo.txt",
				Mode:     object.ModePlain,
				ObjectID: blobSum(dummyContent),
			},
		}.SHA1(),
		Parents:    []Hash{commit0.SHA1()},
		Author:     wantAuthor,
		Committer:  wantCommitter,
		AuthorTime: wantTime1,
		CommitTime: wantTime1,
		Message:    wantMessage1,
	}
	want := []*object.Commit{commit1}
	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("log diff (-want +got):\n%s", diff)
	}
}

func equateTruncatedTime(d time.Duration) cmp.Option {
	return cmp.Comparer(func(t1, t2 time.Time) bool {
		return t1.Truncate(d).Equal(t2.Truncate(d))
	})
}

func TestMuxWriter(t *testing.T) {
	tests := []struct {
		name      string
		bufSize   int
		writes    []string
		flush     bool
		want      string
		wantError bool
	}{
		{
			name: "Empty",
			want: "",
		},
		{
			name:   "SingleLine/InOneWrite",
			writes: []string{"foo\n"},
			want:   "foo\n",
		},
		{
			name:   "SingleLine/ByteAtATime",
			writes: []string{"f", "o", "o", "\n"},
			want:   "foo\n",
		},
		{
			name:   "TrailingData/WithoutFlush",
			writes: []string{"foo\nbar"},
			want:   "foo\n",
		},
		{
			name:   "TrailingData/WithFlush",
			writes: []string{"foo\nbar"},
			flush:  true,
			want:   "foo\nbar",
		},
		{
			name:      "TrailingData/LargerThanBuffer",
			bufSize:   2,
			writes:    []string{"foo\nbar"},
			flush:     true,
			want:      "foo\n",
			wantError: true,
		},
		{
			name:    "LineLargerThanBuffer/InOneWrite",
			bufSize: 2,
			writes:  []string{"Hi!\n"},
			want:    "Hi!\n",
		},
		{
			name:      "LineLargerThanBuffer/ByteAtATime",
			bufSize:   2,
			writes:    []string{"H", "i", "!"},
			wantError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := new(strings.Builder)
			defer func() {
				if got.String() != test.want {
					t.Errorf("got = %q; want %q", got, test.want)
				}
			}()
			mux := &muxWriter{w: got}
			stream := mux.newHandle()
			if test.bufSize != 0 {
				stream.buf = make([]byte, 0, test.bufSize)
			}
			gotError := false
			for i, data := range test.writes {
				n, err := stream.Write([]byte(data))
				if err != nil {
					t.Logf("writes[%d] error: %v", i, err)
					gotError = true
					if !test.wantError || i < len(test.writes)-1 {
						t.Fail()
					}
					break
				}
				if n != len(data) {
					t.Errorf("writes[%d] n = %d; want %d", i, n, len(data))
				}
			}
			if test.flush {
				if err := stream.Flush(); err != nil {
					t.Log("Flush:", err)
					gotError = true
					if !test.wantError {
						t.Fail()
					}
				}
			}
			if !gotError && test.wantError {
				t.Error("No error returned")
			}
		})
	}
}
