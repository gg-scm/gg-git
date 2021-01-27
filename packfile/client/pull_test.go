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

package client

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"gg-scm.io/pkg/git"
	"gg-scm.io/pkg/git/githash"
	"gg-scm.io/pkg/git/object"
	"gg-scm.io/pkg/git/packfile"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestPull(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	localGit, err := git.NewLocal(git.Options{
		Dir: dir,
	})
	if err != nil {
		t.Skip("Can't find Git, skipping:", err)
	}
	g := git.Custom(dir, localGit, localGit)
	objects, err := initPullTestRepository(ctx, g, dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, transport := range allTransportVariants(localGit.Exe()) {
		t.Run(transport.name, func(t *testing.T) {
			for version := 1; version <= 2; version++ {
				t.Run(fmt.Sprintf("Version%d", version), func(t *testing.T) {
					u := transport.getURL(t, dir)
					runPullTest(ctx, t, u, version, objects)
				})
			}
		})
	}
}

func runPullTest(ctx context.Context, t *testing.T, u *url.URL, version int, objects *pullTestObjects) {
	remote, err := NewRemote(u, nil)
	if err != nil {
		t.Fatal("NewRemote:", err)
	}
	if version == 1 {
		remote.pullExtraParams = v1ExtraParams
	}
	stream, err := remote.StartPull(ctx)
	if err != nil {
		t.Fatal("remote.StartPull:", err)
	}
	defer func() {
		if err := stream.Close(); err != nil {
			t.Error("stream.Close():", err)
		}
	}()

	t.Run("ListRefs", func(t *testing.T) {
		got, err := stream.ListRefs()
		if err != nil {
			t.Fatal("ListRefs:", err)
		}
		want := []*Ref{
			{
				Name:         githash.Head,
				ObjectID:     objects.commit2.SHA1(),
				SymrefTarget: objects.mainRef,
			},
			{
				Name:     objects.mainRef,
				ObjectID: objects.commit2.SHA1(),
			},
			{
				Name:     objects.ref1,
				ObjectID: objects.commit1.SHA1(),
			},
			{
				Name:     objects.ref2,
				ObjectID: objects.commit2.SHA1(),
			},
		}
		diff := cmp.Diff(
			want, got,
			cmpopts.SortSlices(func(r1, r2 *Ref) bool { return r1.Name < r2.Name }),
		)
		if diff != "" {
			t.Errorf("ListRefs() (-want +got):\n%s", diff)
		}
	})

	t.Run("Negotiate/All", func(t *testing.T) {
		resp, err := stream.Negotiate(&PullRequest{
			Want: []githash.SHA1{objects.commit2.SHA1()},
		})
		if err != nil {
			t.Fatal("stream.Negotiate:", err)
		}
		if resp.Packfile == nil {
			t.Fatal("stream.Negotiate returned nil Packfile")
		}
		defer func() {
			if err := resp.Packfile.Close(); err != nil {
				t.Error("resp.Packfile.Close():", err)
			}
		}()
		got, err := readPackfile(bufio.NewReader(resp.Packfile))
		if err != nil {
			t.Error(err)
		}
		want := map[githash.SHA1][]byte{
			objects.blobObjectID(): objects.blobContent,
			objects.tree1.SHA1():   mustMarshalBinary(t, objects.tree1),
			objects.commit1.SHA1(): mustMarshalBinary(t, objects.commit1),
			objects.tree2.SHA1():   mustMarshalBinary(t, objects.tree2),
			objects.commit2.SHA1(): mustMarshalBinary(t, objects.commit2),
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("objects (-want +got):\n%s", diff)
		}
	})

	t.Run("Negotiate/First", func(t *testing.T) {
		resp, err := stream.Negotiate(&PullRequest{
			Want: []githash.SHA1{objects.commit1.SHA1()},
		})
		if err != nil {
			t.Fatal("stream.Negotiate:", err)
		}
		if resp.Packfile == nil {
			t.Fatal("stream.Negotiate returned nil Packfile")
		}
		defer func() {
			if err := resp.Packfile.Close(); err != nil {
				t.Error("resp.Packfile.Close():", err)
			}
		}()
		got, err := readPackfile(bufio.NewReader(resp.Packfile))
		if err != nil {
			t.Error(err)
		}
		want := map[githash.SHA1][]byte{
			objects.blobObjectID(): objects.blobContent,
			objects.tree1.SHA1():   mustMarshalBinary(t, objects.tree1),
			objects.commit1.SHA1(): mustMarshalBinary(t, objects.commit1),
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("objects (-want +got):\n%s", diff)
		}
	})

	t.Run("Negotiate/Incremental", func(t *testing.T) {
		resp, err := stream.Negotiate(&PullRequest{
			Want: []githash.SHA1{objects.commit2.SHA1()},
			Have: []githash.SHA1{objects.commit1.SHA1()},
		})
		if err != nil {
			t.Fatal("stream.Negotiate:", err)
		}
		if resp.Packfile == nil {
			t.Fatal("stream.Negotiate returned nil Packfile")
		}
		defer func() {
			if err := resp.Packfile.Close(); err != nil {
				t.Error("resp.Packfile.Close():", err)
			}
		}()
		got, err := readPackfile(bufio.NewReader(resp.Packfile))
		if err != nil {
			t.Error(err)
		}
		want := map[githash.SHA1][]byte{
			objects.tree2.SHA1():   mustMarshalBinary(t, objects.tree2),
			objects.commit2.SHA1(): mustMarshalBinary(t, objects.commit2),
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("objects (-want +got):\n%s", diff)
		}
	})

	t.Run("Negotiate/ShallowSecond", func(t *testing.T) {
		resp, err := stream.Negotiate(&PullRequest{
			Want:  []githash.SHA1{objects.commit2.SHA1()},
			Depth: 1,
		})
		if err != nil {
			t.Fatal("stream.Negotiate:", err)
		}
		if resp.Packfile == nil {
			t.Fatal("stream.Negotiate returned nil Packfile")
		}
		defer func() {
			if err := resp.Packfile.Close(); err != nil {
				t.Error("resp.Packfile.Close():", err)
			}
		}()
		got, err := readPackfile(bufio.NewReader(resp.Packfile))
		if err != nil {
			t.Error(err)
		}
		want := map[githash.SHA1][]byte{
			objects.blobObjectID(): objects.blobContent,
			objects.tree2.SHA1():   mustMarshalBinary(t, objects.tree2),
			objects.commit2.SHA1(): mustMarshalBinary(t, objects.commit2),
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("objects (-want +got):\n%s", diff)
		}
	})

	t.Run("Negotiate/HaveMore", func(t *testing.T) {
		randomHash, err := githash.ParseSHA1("ccfe3cfa687f0ea735937a81454d21fef86cdce8")
		if err != nil {
			t.Fatal(err)
		}
		resp, err := stream.Negotiate(&PullRequest{
			Want:     []githash.SHA1{objects.commit2.SHA1()},
			Have:     []githash.SHA1{randomHash},
			HaveMore: true,
		})
		if err != nil {
			t.Fatal("stream.Negotiate:", err)
		}
		if resp.Packfile != nil {
			resp.Packfile.Close()
			t.Fatal("stream.Negotiate returned non-nil Packfile")
		}
		if _, remoteHasRandomHash := resp.Acks[randomHash]; remoteHasRandomHash {
			t.Error("Remote acknowledged random hash")
		}
	})
}

type pullTestObjects struct {
	mainRef     githash.Ref
	blobContent []byte
	tree1       object.Tree
	tree2       object.Tree
	commit1     *object.Commit
	commit2     *object.Commit
	ref1        githash.Ref
	ref2        githash.Ref
}

func initPullTestRepository(ctx context.Context, g *git.Git, dir string) (*pullTestObjects, error) {
	g = g.WithDir(dir)
	if err := g.Init(ctx, "."); err != nil {
		return nil, err
	}
	mainRef, err := g.HeadRef(ctx)
	if err != nil {
		return nil, err
	}
	const filename1 = "1.txt"
	const fileContent = "Hello, World!\n"
	err = ioutil.WriteFile(filepath.Join(dir, filename1), []byte(fileContent), 0o666)
	if err != nil {
		return nil, err
	}
	err = g.Add(ctx, []git.Pathspec{git.LiteralPath(filename1)}, git.AddOptions{})
	if err != nil {
		return nil, err
	}
	const commitMessage1 = "Initial import"
	const author object.User = "Octocat <octocat@example.com>"
	commitTime1 := time.Date(2020, time.January, 9, 14, 50, 0, 0, time.FixedZone("-0800", -8*60*60))
	err = g.Commit(ctx, commitMessage1, git.CommitOptions{
		Author:     author,
		AuthorTime: commitTime1,
		Committer:  author,
		CommitTime: commitTime1,
	})
	if err != nil {
		return nil, err
	}
	const tag1 = "tag1"
	if err := g.Run(ctx, "tag", tag1); err != nil {
		return nil, err
	}

	const filename2 = "2.txt"
	err = ioutil.WriteFile(filepath.Join(dir, filename2), []byte(fileContent), 0o666)
	if err != nil {
		return nil, err
	}
	err = g.Add(ctx, []git.Pathspec{git.LiteralPath(filename2)}, git.AddOptions{})
	if err != nil {
		return nil, err
	}
	const commitMessage2 = "Added another file"
	commitTime2 := time.Date(2020, time.January, 9, 15, 25, 0, 0, time.FixedZone("-0800", -8*60*60))
	err = g.Commit(ctx, commitMessage2, git.CommitOptions{
		Author:     author,
		AuthorTime: commitTime2,
		Committer:  author,
		CommitTime: commitTime2,
	})
	if err != nil {
		return nil, err
	}
	const tag2 = "tag2"
	if err := g.Run(ctx, "tag", tag2); err != nil {
		return nil, err
	}

	objects := &pullTestObjects{
		mainRef:     mainRef,
		blobContent: []byte(fileContent),
		ref1:        git.TagRef(tag1),
		ref2:        git.TagRef(tag2),
	}
	objects.tree1 = object.Tree{
		{
			Name:     filename1,
			Mode:     object.ModePlain,
			ObjectID: objects.blobObjectID(),
		},
	}
	objects.commit1 = &object.Commit{
		Tree:       objects.tree1.SHA1(),
		Author:     author,
		AuthorTime: commitTime1,
		Committer:  author,
		CommitTime: commitTime1,
		Message:    commitMessage1,
	}
	objects.tree2 = object.Tree{
		{
			Name:     filename1,
			Mode:     object.ModePlain,
			ObjectID: objects.blobObjectID(),
		},
		{
			Name:     filename2,
			Mode:     object.ModePlain,
			ObjectID: objects.blobObjectID(),
		},
	}
	objects.commit2 = &object.Commit{
		Tree:       objects.tree2.SHA1(),
		Parents:    []githash.SHA1{objects.commit1.SHA1()},
		Author:     author,
		AuthorTime: commitTime2,
		Committer:  author,
		CommitTime: commitTime2,
		Message:    commitMessage2,
	}
	return objects, nil
}

func (objects *pullTestObjects) blobObjectID() githash.SHA1 {
	id, err := object.BlobSum(bytes.NewReader(objects.blobContent), int64(len(objects.blobContent)))
	if err != nil {
		panic(err)
	}
	return id
}

func TestPullGitHubRepository(t *testing.T) {
	t.Skip("Not hermetic. Test intended for manual development.")
	r, err := NewRemote(&url.URL{
		Scheme: "https",
		Host:   "github.com",
		Path:   "/git/git.git",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	stream, err := r.StartPull(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	refs, err := stream.ListRefs("refs/heads/")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range refs {
		t.Log(r.Name.Branch())
	}
}

func readPackfile(r packfile.ByteReader) (map[githash.SHA1][]byte, error) {
	pr := packfile.NewReader(r)
	objects := make(map[githash.SHA1][]byte)
	for {
		hdr, err := pr.Next()
		if errors.Is(err, io.EOF) {
			return objects, nil
		}
		if err != nil {
			return objects, err
		}
		var objType object.Type
		switch hdr.Type {
		case packfile.Blob:
			objType = object.TypeBlob
		case packfile.Tree:
			objType = object.TypeTree
		case packfile.Commit:
			objType = object.TypeCommit
		case packfile.Tag:
			objType = object.TypeTag
		default:
			return objects, fmt.Errorf("unsupported object type %v", hdr.Type)
		}
		h := sha1.New()
		h.Write(object.AppendPrefix(nil, objType, hdr.Size))
		buf := new(bytes.Buffer)
		if _, err := io.Copy(io.MultiWriter(buf, h), pr); err != nil {
			return objects, err
		}
		var sum githash.SHA1
		h.Sum(sum[:0])
		objects[sum] = buf.Bytes()
	}
}

func mustMarshalBinary(tb testing.TB, m encoding.BinaryMarshaler) []byte {
	data, err := m.MarshalBinary()
	if err != nil {
		tb.Fatal("MarshalBinary:", err)
	}
	return data
}

type transportVariant struct {
	name   string
	getURL func(tb testing.TB, dir string) *url.URL
}

func allTransportVariants(gitExe string) []transportVariant {
	return []transportVariant{
		{"Local", func(_ testing.TB, dir string) *url.URL {
			return URLFromPath(dir)
		}},
		{"HTTP", func(tb testing.TB, dir string) *url.URL {
			httpServer := serveHTTPRepository(gitExe, dir)
			tb.Cleanup(httpServer.Close)
			u, err := ParseURL(httpServer.URL)
			if err != nil {
				tb.Fatal(err)
			}
			return u
		}},
	}
}

func serveHTTPRepository(gitExe string, dir string) *httptest.Server {
	// https://git-scm.com/docs/git-http-backend
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TransferEncoding) > 0 && r.TransferEncoding[0] == "chunked" {
			// Go's net/http/cgi server doesn't support chunked encoding, which is
			// a non-standard CGI feature that Apache supports. https://golang.org/issue/5613
			// We're okay with a really inefficient implementation for tests.
			bodyFile, err := ioutil.TempFile("", "git-server-body")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer func() {
				path := bodyFile.Name()
				bodyFile.Close()
				os.Remove(path)
			}()
			size, err := io.Copy(bodyFile, r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if _, err := bodyFile.Seek(0, io.SeekStart); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			r = r.Clone(r.Context())
			r.TransferEncoding = nil
			r.Header.Set("Content-Length", strconv.FormatInt(size, 10))
			r.Body = bodyFile
		}
		gitServer := &cgi.Handler{
			Dir:  dir,
			Path: gitExe,
			Args: []string{"-c", "http.receivepack=true", "http-backend"},
			Env: []string{
				"GIT_HTTP_EXPORT_ALL=true",
				"GIT_PROJECT_ROOT=" + dir,
				// Bafflingly, git-http-backend does not set GIT_PROTOCOL from
				// HTTP_GIT_PROTOCOL. The canonical solution is to have the CGI
				// environment do it.
				// https://github.com/git/git/blob/74b082ad34fe2c727c676dac5c33d5e1e5f5ca56/t/lib-httpd/apache.conf#L84-L84
				"GIT_PROTOCOL=" + r.Header.Get("Git-Protocol"),
			},
		}
		gitServer.ServeHTTP(w, r)
	}))
}
