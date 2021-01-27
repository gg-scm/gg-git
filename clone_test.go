// Copyright 2021 The gg Authors
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

package git

import (
	"context"
	"testing"

	"gg-scm.io/pkg/git/internal/filesystem"
)

func TestClone(t *testing.T) {
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

	// Create repository A with a commit.
	if err := env.g.Init(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	gitA := env.g.WithDir("a")
	if err := env.root.Apply(filesystem.Write("a/foo.txt", dummyContent)); err != nil {
		t.Fatal(err)
	}
	if err := gitA.Add(ctx, []Pathspec{"foo.txt"}, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := gitA.Commit(ctx, "First commit", CommitOptions{}); err != nil {
		t.Fatal(err)
	}
	wantRev, err := gitA.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Clone repository A to directory B.
	if err := env.g.Clone(ctx, URLFromPath("a"), CloneOptions{Dir: "b"}); err != nil {
		t.Error("Clone:", err)
	}
	gitB := env.g.WithDir("b")

	gotRev, err := gitB.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if gotRev.Commit != wantRev.Commit {
		t.Errorf("b HEAD = %v; want %v", gotRev.Commit, wantRev.Commit)
	}
	cfg, err := gitB.ReadConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if isBare, err := cfg.Bool("core.bare"); err != nil {
		t.Error(err)
	} else if isBare {
		t.Error("Cloned repository is bare")
	}
}

func TestCloneBare(t *testing.T) {
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

	// Create repository A with a commit.
	if err := env.g.Init(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	gitA := env.g.WithDir("a")
	if err := env.root.Apply(filesystem.Write("a/foo.txt", dummyContent)); err != nil {
		t.Fatal(err)
	}
	if err := gitA.Add(ctx, []Pathspec{"foo.txt"}, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := gitA.Commit(ctx, "First commit", CommitOptions{}); err != nil {
		t.Fatal(err)
	}
	wantRev, err := gitA.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Clone repository A to directory B.
	if err := env.g.CloneBare(ctx, URLFromPath("a"), CloneOptions{Dir: "b.git"}); err != nil {
		t.Error("Clone:", err)
	}
	gitB := env.g.WithDir("b.git")

	gotRev, err := gitB.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if gotRev.Commit != wantRev.Commit {
		t.Errorf("b HEAD = %v; want %v", gotRev.Commit, wantRev.Commit)
	}
	cfg, err := gitB.ReadConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if isBare, err := cfg.Bool("core.bare"); err != nil {
		t.Error(err)
	} else if !isBare {
		t.Error("Cloned repository is not bare")
	}
}
