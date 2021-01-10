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

/*
Package client provides a Git packfile protocol client for sending and receiving
Git objects. The packfile protocol is used in the `git fetch` and `git push`
commands. See https://git-scm.com/docs/pack-protocol for more detailed information.
*/
package client

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"

	"gg-scm.io/pkg/git/githash"
	"gg-scm.io/pkg/git/internal/giturl"
)

// Remote represents a Git repository that can be pulled from or pushed to.
type Remote struct {
	urlstr           string
	impl             impl
	fetchExtraParams string
}

// Options holds optional arguments for creating a Remote.
type Options struct {
	HTTPClient        *http.Client // defaults to http.DefaultClient
	HTTPAuthorization string
	UserAgent         string
}

func (opts *Options) httpClient() *http.Client {
	if opts == nil || opts.HTTPClient == nil {
		return http.DefaultClient
	}
	return opts.HTTPClient
}

func (opts *Options) httpAuthorization() string {
	if opts == nil {
		return ""
	}
	return opts.HTTPAuthorization
}

func (opts *Options) httpUserAgent() string {
	if opts == nil {
		return ""
	}
	return opts.UserAgent
}

// NewRemote returns a new Remote or returns an error if the transport specified
// in the URL scheme is unsupported.
func NewRemote(u *url.URL, opts *Options) (*Remote, error) {
	urlstr := u.Redacted()
	remote := &Remote{
		urlstr:           urlstr,
		fetchExtraParams: v2ExtraParams,
	}
	switch u.Scheme {
	case "", "file":
		if u.Host != "localhost" && u.Host != "" {
			return nil, fmt.Errorf("open remote %s: cannot use a host with file://", urlstr)
		}
		uploadPackPath, err := exec.LookPath("git-upload-pack")
		if err != nil {
			return nil, fmt.Errorf("open remote %s: %w", urlstr, err)
		}
		receivePackPath, err := exec.LookPath("git-receive-pack")
		if err != nil {
			return nil, fmt.Errorf("open remote %s: %w", urlstr, err)
		}
		remote.impl = &fileRemote{
			dir:             u.Path,
			uploadPackPath:  uploadPackPath,
			receivePackPath: receivePackPath,
		}
	case "http", "https":
		auth := opts.httpAuthorization()
		if u.User != nil {
			auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(u.User.String()))
		}
		remote.impl = &httpRemote{
			client:        opts.httpClient(),
			base:          u,
			authorization: auth,
			userAgent:     opts.httpUserAgent(),
		}
	default:
		return nil, fmt.Errorf("open remote %s: unknown scheme %q", urlstr, u.Scheme)
	}
	return remote, nil
}

func parseObjectID(src []byte) (githash.SHA1, error) {
	var id githash.SHA1
	if err := id.UnmarshalText(src); err != nil {
		return githash.SHA1{}, fmt.Errorf("parse object id: %w", err)
	}
	return id, nil
}

type impl interface {
	advertiseRefs(ctx context.Context, extraParams string) (io.ReadCloser, error)
	uploadPack(ctx context.Context, extraParams string, request io.Reader) (io.ReadCloser, error)
	receivePack(ctx context.Context) (receivePackConn, error)
}

type receivePackConn interface {
	io.Reader
	io.Writer
	CloseWrite() error
	io.Closer
}

// ParseURL parses a Git remote URL, including the alternative SCP syntax.
// See git-fetch(1) for details.
func ParseURL(urlstr string) (*url.URL, error) {
	return giturl.Parse(urlstr)
}
