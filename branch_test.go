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
	"testing"

	"gg-scm.io/pkg/git/internal/filesystem"
)

func TestNewBranch(t *testing.T) {
	gitPath, err := findGit()
	if err != nil {
		t.Skip("git not found:", err)
	}
	const (
		content1 = "Hello World!\n"
		content2 = "Next step\n"
		content3 = "Out there on the frontiers\n"
	)
	tests := []struct {
		name string

		branch string
		opts   BranchOptions

		wantErr          bool
		wantBranchCommit string // revision that matches
		wantHEAD         Ref
		wantLocalContent string
		wantUpstream     Ref
	}{
		{
			name:             "Defaults",
			branch:           "foo",
			wantBranchCommit: "main",
			wantHEAD:         "refs/heads/main",
			wantLocalContent: content2,
		},
		{
			name:   "StartPoint",
			branch: "foo",
			opts: BranchOptions{
				StartPoint: "existing",
			},
			wantBranchCommit: "existing",
			wantHEAD:         "refs/heads/main",
			wantLocalContent: content2,
		},
		{
			name:   "Checkout",
			branch: "foo",
			opts: BranchOptions{
				Checkout: true,
			},
			wantBranchCommit: "main",
			wantHEAD:         "refs/heads/foo",
			wantLocalContent: content2,
		},
		{
			name:   "CheckoutSameCommit",
			branch: "foo",
			opts: BranchOptions{
				StartPoint: "HEAD",
				Checkout:   true,
			},
			wantBranchCommit: "main",
			wantHEAD:         "refs/heads/foo",
			wantLocalContent: content2,
		},
		{
			name:   "CheckoutDifferentCommit",
			branch: "foo",
			opts: BranchOptions{
				StartPoint: "existing",
				Checkout:   true,
			},
			wantBranchCommit: "existing",
			wantHEAD:         "refs/heads/foo",
			wantLocalContent: content3,
		},
		{
			name:             "NameCollision",
			branch:           "existing",
			wantErr:          true,
			wantBranchCommit: "existing",
			wantHEAD:         "refs/heads/main",
			wantLocalContent: content2,
			wantUpstream:     "refs/heads/main",
		},
		{
			name:   "Overwrite",
			branch: "existing",
			opts: BranchOptions{
				Overwrite: true,
			},
			wantBranchCommit: "main",
			wantHEAD:         "refs/heads/main",
			wantLocalContent: content2,
			wantUpstream:     "refs/heads/main",
		},
		{
			name:   "OverwriteCheckout",
			branch: "existing",
			opts: BranchOptions{
				Overwrite: true,
				Checkout:  true,
			},
			wantBranchCommit: "main",
			wantHEAD:         "refs/heads/existing",
			wantLocalContent: content2,
			wantUpstream:     "refs/heads/main",
		},
		{
			name:   "OverwriteCheckoutNonHEAD",
			branch: "existing",
			opts: BranchOptions{
				StartPoint: "HEAD~",
				Overwrite:  true,
				Checkout:   true,
			},
			wantBranchCommit: "main~",
			wantHEAD:         "refs/heads/existing",
			wantLocalContent: content1,
			wantUpstream:     "refs/heads/main",
		},
		{
			name:   "TrackEmptyStartPoint",
			branch: "foo",
			opts: BranchOptions{
				Track: true,
			},
			wantBranchCommit: "main",
			wantHEAD:         "refs/heads/main",
			wantLocalContent: content2,
			wantUpstream:     "refs/heads/main",
		},
		{
			name:   "Track",
			branch: "foo",
			opts: BranchOptions{
				StartPoint: "existing",
				Track:      true,
			},
			wantBranchCommit: "existing",
			wantHEAD:         "refs/heads/main",
			wantLocalContent: content2,
			wantUpstream:     "refs/heads/existing",
		},
		{
			name:   "CheckoutTrackEmptyStartPoint",
			branch: "foo",
			opts: BranchOptions{
				Checkout: true,
				Track:    true,
			},
			wantBranchCommit: "main",
			wantHEAD:         "refs/heads/foo",
			wantLocalContent: content2,
			wantUpstream:     "refs/heads/main",
		},
		{
			name:   "CheckoutTrack",
			branch: "foo",
			opts: BranchOptions{
				StartPoint: "existing",
				Checkout:   true,
				Track:      true,
			},
			wantBranchCommit: "existing",
			wantHEAD:         "refs/heads/foo",
			wantLocalContent: content3,
			wantUpstream:     "refs/heads/existing",
		},
	}

	ctx := context.Background()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env, err := newTestEnv(ctx, gitPath)
			if err != nil {
				t.Fatal(err)
			}
			defer env.cleanup()

			// Create a repository with three commits: main points to the
			// second and existing points to the third.
			if err := env.g.Init(ctx, "."); err != nil {
				t.Fatal(err)
			}
			if err := env.root.Apply(filesystem.Write("file.txt", content1)); err != nil {
				t.Fatal(err)
			}
			if err := env.g.Add(ctx, []Pathspec{"file.txt"}, AddOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := env.g.Commit(ctx, dummyContent, CommitOptions{}); err != nil {
				t.Fatal(err)
			}
			commit1, err := env.g.Head(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := env.root.Apply(filesystem.Write("file.txt", content2)); err != nil {
				t.Fatal(err)
			}
			if err := env.g.CommitAll(ctx, dummyContent, CommitOptions{}); err != nil {
				t.Fatal(err)
			}
			commit2, err := env.g.Head(ctx)
			if err != nil {
				t.Fatal(err)
			}
			// Use raw commands to avoid relying on system-under-test.
			if err := env.g.Run(ctx, "checkout", "--quiet", "-b", "existing"); err != nil {
				t.Fatal(err)
			}
			if err := env.g.Run(ctx, "branch", "--set-upstream-to=main"); err != nil {
				t.Fatal(err)
			}
			if err := env.root.Apply(filesystem.Write("file.txt", content3)); err != nil {
				t.Fatal(err)
			}
			if err := env.g.CommitAll(ctx, dummyContent, CommitOptions{}); err != nil {
				t.Fatal(err)
			}
			commit3, err := env.g.Head(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := env.g.CheckoutBranch(ctx, "main", CheckoutOptions{}); err != nil {
				t.Fatal(err)
			}
			// Store expected commit before system-under-test modifies.
			wantBranchRev, err := env.g.ParseRev(ctx, test.wantBranchCommit)
			if err != nil {
				t.Fatal(err)
			}

			// Call NewBranch.
			if err := env.g.NewBranch(ctx, test.branch, test.opts); err != nil && !test.wantErr {
				t.Error("NewBranch:", err)
			} else if err == nil && test.wantErr {
				t.Error("NewBranch did not return an error")
			}

			// Verify that the branch ref points to the expected commit.
			branchRef := BranchRef(test.branch)
			if r, err := env.g.ParseRev(ctx, branchRef.String()); err != nil {
				t.Error(err)
			} else if r.Commit != wantBranchRev.Commit {
				names := map[Hash]string{
					commit1.Commit: "commit 1",
					commit2.Commit: "main/commit 2",
					commit3.Commit: "existing/commit 3",
				}
				t.Errorf("%v = %v; want %v", branchRef, prettyCommit(r.Commit, names), prettyCommit(wantBranchRev.Commit, names))
			}

			// Verify that HEAD points to the expected ref.
			if head, err := env.g.Head(ctx); err != nil {
				t.Error(err)
			} else if head.Ref != test.wantHEAD {
				t.Errorf("HEAD = %v; want %v", head.Ref, test.wantHEAD)
			}

			// Verify that the file content matches the expected content.
			if got, err := env.root.ReadFile("file.txt"); err != nil {
				t.Error(err)
			} else if got != test.wantLocalContent {
				t.Errorf("file.txt content = %q; want %q", got, test.wantLocalContent)
			}

			// Verify that the branch's upstream matches the expected ref.
			gotUpstream := Ref("")
			if r, err := env.g.ParseRev(ctx, test.branch+"@{upstream}"); err == nil {
				gotUpstream = r.Ref
			}
			if gotUpstream != test.wantUpstream {
				t.Errorf("%s upstream = %q; want %q", test.branch, gotUpstream, test.wantUpstream)
			}
		})
	}
}

func TestDeleteBranches(t *testing.T) {
	ctx := context.Background()
	gitPath, err := findGit()
	if err != nil {
		t.Skip("git not found:", err)
	}
	env, err := newTestEnv(ctx, gitPath)
	if err != nil {
		t.Fatal(err)
	}
	defer env.cleanup()

	// Create a repository with a commit and a branch "foo".
	if err := env.g.Init(ctx, "."); err != nil {
		t.Fatal(err)
	}
	const content = "Hello, World!\n"
	if err := env.root.Apply(filesystem.Write("file.txt", content)); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Add(ctx, []Pathspec{"file.txt"}, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := env.g.Commit(ctx, dummyContent, CommitOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := env.g.NewBranch(ctx, "foo", BranchOptions{}); err != nil {
		t.Fatal(err)
	}

	// Delete the branch "foo".
	if err := env.g.DeleteBranches(ctx, []string{"foo"}, DeleteBranchOptions{}); err != nil {
		t.Error(err)
	}

	// Verify that "foo" no longer exists.
	refs, err := env.g.ListRefs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const fooRef = Ref("refs/heads/foo")
	if hash, ok := refs[fooRef]; ok {
		t.Errorf("%s = %v; want missing", fooRef, hash)
	}
}
