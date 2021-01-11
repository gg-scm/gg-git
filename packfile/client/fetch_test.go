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
	"strings"
	"testing"
	"time"

	"gg-scm.io/pkg/git"
	"gg-scm.io/pkg/git/githash"
	"gg-scm.io/pkg/git/object"
	"gg-scm.io/pkg/git/packfile"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestFetch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	localGit, err := git.NewLocal(git.Options{
		Dir: dir,
	})
	if err != nil {
		t.Skip("Can't find Git, skipping:", err)
	}
	g := git.Custom(dir, localGit, localGit)
	if err := g.Init(ctx, "."); err != nil {
		t.Fatal(err)
	}
	mainRef, err := g.HeadRef(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const fname = "foo.txt"
	const fileContent = "Hello, World!\n"
	err = ioutil.WriteFile(filepath.Join(dir, fname), []byte(fileContent), 0o666)
	if err != nil {
		t.Fatal(err)
	}
	err = g.Add(ctx, []git.Pathspec{git.LiteralPath(fname)}, git.AddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	const commitMessage = "Initial import"
	const author object.User = "Octocat <octocat@example.com>"
	commitTime := time.Date(2020, time.January, 9, 14, 50, 0, 0, time.FixedZone("-0800", -8*60*60))
	err = g.Commit(ctx, commitMessage, git.CommitOptions{
		Author:     author,
		AuthorTime: commitTime,
		Committer:  author,
		CommitTime: commitTime,
	})
	if err != nil {
		t.Fatal(err)
	}

	blobObjectID, err := object.BlobSum(strings.NewReader(fileContent), int64(len(fileContent)))
	if err != nil {
		t.Fatal(err)
	}
	treeObject := object.Tree{
		{
			Name:     fname,
			Mode:     object.ModePlain,
			ObjectID: blobObjectID,
		},
	}
	commitObject := &object.Commit{
		Tree:       treeObject.SHA1(),
		Author:     author,
		AuthorTime: commitTime,
		Committer:  author,
		CommitTime: commitTime,
		Message:    commitMessage,
	}

	for _, transport := range allTransportVariants(localGit.Exe()) {
		t.Run(transport.name, func(t *testing.T) {
			for version := 1; version <= 2; version++ {
				t.Run(fmt.Sprintf("Version%d", version), func(t *testing.T) {
					remote, err := NewRemote(transport.getURL(t, dir), nil)
					if err != nil {
						t.Fatal("NewRemote:", err)
					}
					if version == 1 {
						remote.fetchExtraParams = v1ExtraParams
					}
					stream, err := remote.StartFetch(ctx)
					if err != nil {
						t.Fatal("remote.StartFetch:", err)
					}
					defer func() {
						if err := stream.Close(); err != nil {
							t.Error("stream.Close():", err)
						}
					}()
					if gotRefs, err := stream.ListRefs(); err != nil {
						t.Error("ListRefs:", err)
					} else {
						wantHeadTarget := mainRef
						wantRefs := []*Ref{
							{
								Name:         githash.Head,
								ObjectID:     commitObject.SHA1(),
								SymrefTarget: wantHeadTarget,
							},
							{
								Name:     mainRef,
								ObjectID: commitObject.SHA1(),
							},
						}
						diff := cmp.Diff(
							wantRefs, gotRefs,
							cmpopts.SortSlices(func(r1, r2 *Ref) bool { return r1.Name < r2.Name }),
						)
						if diff != "" {
							t.Errorf("ListRefs() (-want +got):\n%s", diff)
						}
					}
					resp, err := stream.Negotiate(&FetchRequest{
						Want: []githash.SHA1{commitObject.SHA1()},
					})
					if err != nil {
						t.Fatal("stream.Negotiate:", err)
					}
					if resp.Packfile == nil {
						t.Fatal("stream.Negotiate returned nil Packfile")
					}
					defer func() {
						if err := resp.Packfile.Close(); err != nil {
							t.Error("stream.Close():", err)
						}
					}()
					got, err := readPackfile(bufio.NewReader(resp.Packfile))
					if err != nil {
						t.Error(err)
					}
					want := map[githash.SHA1][]byte{
						blobObjectID:        []byte(fileContent),
						treeObject.SHA1():   mustMarshalBinary(t, treeObject),
						commitObject.SHA1(): mustMarshalBinary(t, commitObject),
					}
					if diff := cmp.Diff(want, got); diff != "" {
						t.Errorf("objects (-want +got):\n%s", diff)
					}
				})
			}
		})
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
			return &url.URL{
				Scheme: "file",
				Path:   filepath.FromSlash(dir),
			}
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
	gitServer := &cgi.Handler{
		Dir:  dir,
		Path: gitExe,
		Args: []string{"-c", "http.receivepack=true", "http-backend"},
		Env: []string{
			"GIT_HTTP_EXPORT_ALL=true",
			"GIT_PROJECT_ROOT=" + dir,
		},
	}
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
		gitServer.ServeHTTP(w, r)
	}))
}
