// Copyright 2020 The gg Authors
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
//
// SPDX-License-Identifier: Apache-2.0

package git_test

import (
	"context"
	"fmt"
	"io/ioutil"

	"gg-scm.io/pkg/git"
)

func ExampleGit_Head() {
	ctx := context.Background()

	// Find the Git executable.
	g, err := git.New(git.Options{})
	if err != nil {
		// handle error
	}

	// Print currently checked out revision.
	rev, err := g.Head(ctx)
	if err != nil {
		// handle error
	}
	fmt.Println(rev.Commit.Short())
}

func ExampleGit_HeadRef() {
	ctx := context.Background()

	// Find the Git executable.
	g, err := git.New(git.Options{})
	if err != nil {
		// handle error
	}

	// Print currently checked out branch.
	ref, err := g.HeadRef(ctx)
	if err != nil {
		// handle error
	}
	if ref == "" {
		fmt.Println("detached HEAD")
	} else {
		fmt.Println(ref.Branch())
	}
}

func ExampleGit_ParseRev() {
	ctx := context.Background()

	// Find the Git executable.
	g, err := git.New(git.Options{})
	if err != nil {
		// handle error
	}

	// Convert a revision reference into a commit hash.
	rev, err := g.ParseRev(ctx, "v0.1.0")
	if err != nil {
		// handle error
	}
	// Print something like: refs/tags/v0.1.0 - 09f2632a
	fmt.Printf("%s - %s\n", rev.Ref, rev.Commit.Short())
}

func ExampleGit_Commit() {
	ctx := context.Background()

	// Find the Git executable.
	g, err := git.New(git.Options{})
	if err != nil {
		// handle error
	}

	// Write a file and track it with `git add`.
	err = ioutil.WriteFile("foo.txt", []byte("Hello, World!\n"), 0666)
	if err != nil {
		// handle error
	}
	err = g.Add(ctx, []git.Pathspec{git.LiteralPath("foo.txt")}, git.AddOptions{})
	if err != nil {
		// handle error
	}

	// Create a new commit.
	err = g.Commit(ctx, "Added foo.txt with a greeting", git.CommitOptions{})
	if err != nil {
		// handle error
	}
}

func ExampleConfig() {
	ctx := context.Background()

	// Find the Git executable.
	g, err := git.New(git.Options{})
	if err != nil {
		// handle error
	}

	// Read the repository configuration.
	cfg, err := g.ReadConfig(ctx)
	if err != nil {
		// handle error
	}

	// Read a particular configuration value.
	fmt.Println("Hello,", cfg.Value("user.name"))
}

func ExampleRemote() {
	ctx := context.Background()

	// Find the Git executable.
	g, err := git.New(git.Options{})
	if err != nil {
		// handle error
	}

	// Read the repository configuration.
	cfg, err := g.ReadConfig(ctx)
	if err != nil {
		// handle error
	}

	// Get details about a particular remote.
	remotes := cfg.ListRemotes()
	origin := remotes["origin"]
	if origin == nil {
		fmt.Println("No origin remote")
	} else {
		fmt.Println("Fetching from", origin.FetchURL)
	}
}

func ExampleParseURL() {
	u, err := git.ParseURL("https://github.com/octocat/example")
	if err != nil {
		// handle error
	}
	fmt.Printf("HTTP URL: scheme=%s host=%s path=%s\n", u.Scheme, u.Host, u.Path)

	u, err = git.ParseURL("ssh://git@github.com/octocat/example.git")
	if err != nil {
		// handle error
	}
	fmt.Printf("SSH URL: scheme=%s host=%s user=%s path=%s\n", u.Scheme, u.Host, u.User.Username(), u.Path)

	u, err = git.ParseURL("git@github.com:octocat/example.git")
	if err != nil {
		// handle error
	}
	fmt.Printf("SCP URL: scheme=%s host=%s user=%s path=%s\n", u.Scheme, u.Host, u.User.Username(), u.Path)

	// Output:
	// HTTP URL: scheme=https host=github.com path=/octocat/example
	// SSH URL: scheme=ssh host=github.com user=git path=/octocat/example.git
	// SCP URL: scheme=ssh host=github.com user=git path=/octocat/example.git
}
