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
	"io/ioutil"

	"gg-scm.io/pkg/git"
)

func Example() {
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
