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

package client_test

import (
	"bufio"
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"gg-scm.io/pkg/git/githash"
	"gg-scm.io/pkg/git/object"
	"gg-scm.io/pkg/git/packfile"
	"gg-scm.io/pkg/git/packfile/client"
)

func ExampleNewRemote() {
	u, err := client.ParseURL("https://example.com/my/repo.git")
	if err != nil {
		// handle error
	}
	remote, err := client.NewRemote(u, nil)
	if err != nil {
		// handle error
	}
	_ = remote
}

// This example connects to a remote, lists all the refs, and requests for them
// all to be downloaded. This is suitable for performing the initial clone.
func ExampleFetchStream_clone() {
	// Create a remote for the URL.
	ctx := context.Background()
	u, err := client.ParseURL("https://example.com/my/repo.git")
	if err != nil {
		// handle error
	}
	remote, err := client.NewRemote(u, nil)
	if err != nil {
		// handle error
	}

	// Open a connection for fetching objects.
	stream, err := remote.StartFetch(ctx)
	if err != nil {
		// handle error
	}
	defer stream.Close()

	// Gather object IDs to fetch.
	refs, err := stream.ListRefs()
	if err != nil {
		// handle error
	}
	var want []githash.SHA1
	for _, r := range refs {
		want = append(want, r.ObjectID)
	}

	// Start fetching from remote.
	response, err := stream.Negotiate(&client.FetchRequest{
		Want:     want,
		Progress: os.Stdout,
	})
	if err != nil {
		// handle error
	}
	defer response.Packfile.Close()

	// Read the packfile and print commit IDs.
	packReader := packfile.NewReader(bufio.NewReader(response.Packfile))
	for {
		hdr, err := packReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// handle error
		}
		if hdr.Type == packfile.Commit {
			// Hash the object to get the ID.
			h := sha1.New()
			h.Write(object.AppendPrefix(nil, object.TypeCommit, hdr.Size))
			if _, err := io.Copy(h, packReader); err != nil {
				// handle error
			}
			var commitID githash.SHA1
			h.Sum(commitID[:0])
			fmt.Println(commitID)
		}
	}
}

// This example connects to a remote, finds a single desired ref, and requests
// only objects for that ref to be downloaded.
func ExampleFetchStream_singleRef() {
	// Create a remote for the URL.
	ctx := context.Background()
	u, err := client.ParseURL("https://example.com/my/repo.git")
	if err != nil {
		// handle error
	}
	remote, err := client.NewRemote(u, nil)
	if err != nil {
		// handle error
	}

	// Open a connection for fetching objects.
	stream, err := remote.StartFetch(ctx)
	if err != nil {
		// handle error
	}
	defer stream.Close()

	// Find the HEAD ref to fetch.
	refs, err := stream.ListRefs()
	if err != nil {
		// handle error
	}
	var headRef *client.Ref
	for _, r := range refs {
		if r.Name == githash.Head {
			headRef = r
			break
		}
	}
	if headRef == nil {
		fmt.Fprintln(os.Stderr, "No HEAD found!")
		return
	}

	// Start fetching from remote.
	response, err := stream.Negotiate(&client.FetchRequest{
		Want:     []githash.SHA1{headRef.ObjectID},
		Progress: os.Stdout,
	})
	if err != nil {
		// handle error
	}
	defer response.Packfile.Close()

	// Read the packfile and print commit IDs.
	packReader := packfile.NewReader(bufio.NewReader(response.Packfile))
	for {
		hdr, err := packReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// handle error
		}
		if hdr.Type == packfile.Commit {
			// Hash the object to get the ID.
			h := sha1.New()
			h.Write(object.AppendPrefix(nil, object.TypeCommit, hdr.Size))
			if _, err := io.Copy(h, packReader); err != nil {
				// handle error
			}
			var commitID githash.SHA1
			h.Sum(commitID[:0])
			fmt.Println(commitID)
		}
	}
}

// This example connects to a remote and requests only the objects for a
// single commit.
func ExampleFetchStream_singleCommit() {
	// Create a remote for the URL.
	ctx := context.Background()
	u, err := client.ParseURL("https://github.com/gg-scm/gg-git.git")
	if err != nil {
		// handle error
	}
	remote, err := client.NewRemote(u, nil)
	if err != nil {
		// handle error
	}

	// Open a connection for fetching objects.
	stream, err := remote.StartFetch(ctx)
	if err != nil {
		// handle error
	}
	defer stream.Close()
	if !stream.Capabilities().Has(client.FetchCapShallow) {
		fmt.Fprintln(os.Stderr, "Remote does not support shallow clones!")
		return
	}

	// Start fetching from remote.
	want, err := githash.ParseSHA1("c8ede9119a7188f2564d3b7257fa526c9285c23f")
	if err != nil {
		// handle error
	}
	response, err := stream.Negotiate(&client.FetchRequest{
		Want:     []githash.SHA1{want},
		Depth:    1,
		Progress: os.Stderr,
	})
	if err != nil {
		// handle error
	}
	defer response.Packfile.Close()

	// Read the packfile and print commit IDs.
	packReader := packfile.NewReader(bufio.NewReader(response.Packfile))
	for {
		hdr, err := packReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// handle error
		}
		if hdr.Type == packfile.Commit {
			// Hash the object to get the ID.
			h := sha1.New()
			h.Write(object.AppendPrefix(nil, object.TypeCommit, hdr.Size))
			if _, err := io.Copy(h, packReader); err != nil {
				// handle error
			}
			var commitID githash.SHA1
			h.Sum(commitID[:0])
			fmt.Println(commitID)
		}
	}

	// Output:
	// c8ede9119a7188f2564d3b7257fa526c9285c23f
}

func ExamplePushStream() {
	// Create a remote for the URL.
	ctx := context.Background()
	u, err := client.ParseURL("https://example.com/my/repo.git")
	if err != nil {
		// handle error
	}
	remote, err := client.NewRemote(u, nil)
	if err != nil {
		// handle error
	}

	// Open a connection for sending objects.
	stream, err := remote.StartPush(ctx)
	if err != nil {
		// handle error
	}
	defer func() {
		if err := stream.Close(); err != nil {
			// handle error
		}
	}()

	// Find the current commit for the main branch.
	// You should check that the ref is currently pointing to an ancestor of the
	// commit you want to push to that ref. Otherwise, you are performing a
	// force push.
	mainRef := githash.BranchRef("main")
	var curr *client.Ref
	for _, r := range stream.Refs() {
		if r.Name == mainRef {
			curr = r
			break
		}
	}
	if curr == nil {
		fmt.Fprintln(os.Stderr, "main branch not found!")
		return
	}

	// Create a new commit that deletes the entire tree.
	const author = "<foo@example.com>"
	now := time.Now()
	newTree := object.Tree(nil)
	newCommit := &object.Commit{
		Tree:       newTree.SHA1(),
		Parents:    []githash.SHA1{curr.ObjectID},
		Author:     author,
		AuthorTime: now,
		Committer:  author,
		CommitTime: now,
		Message:    "Delete ALL THE THINGS!",
	}

	// Start the push. First inform the remote of refs that we intend to change.
	err = stream.WriteCommands(&client.PushCommand{
		RefName: mainRef,
		Old:     curr.ObjectID,
		New:     newCommit.SHA1(),
	})
	if err != nil {
		// handle error
	}

	// Write a packfile with the new objects to the stream.
	// 1. Write the tree.
	packWriter := packfile.NewWriter(stream, 2)
	treeData, err := newTree.MarshalBinary()
	if err != nil {
		// handle error
	}
	_, err = packWriter.WriteHeader(&packfile.Header{
		Type: packfile.Tree,
		Size: int64(len(treeData)),
	})
	if err != nil {
		// handle error
	}
	if _, err := packWriter.Write(treeData); err != nil {
		// handle error
	}

	// 2. Write the commit.
	commitData, err := newCommit.MarshalBinary()
	if err != nil {
		// handle error
	}
	_, err = packWriter.WriteHeader(&packfile.Header{
		Type: packfile.Commit,
		Size: int64(len(commitData)),
	})
	if err != nil {
		// handle error
	}
	if _, err := packWriter.Write(commitData); err != nil {
		// handle error
	}
}
