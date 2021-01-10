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
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"

	"gg-scm.io/pkg/git/githash"
	"gg-scm.io/pkg/git/internal/giturl"
	"gg-scm.io/pkg/git/internal/pktline"
)

type Remote struct {
	urlstr string
	impl   impl

	uploadCapsMu chan struct{}
	uploadCaps   v2Capabilities
}

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

func NewRemote(u *url.URL, opts *Options) (*Remote, error) {
	urlstr := u.Redacted()
	remote := &Remote{
		urlstr:       urlstr,
		uploadCapsMu: make(chan struct{}, 1),
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

func (r *Remote) ensureUploadCaps(ctx context.Context) (v2Capabilities, error) {
	select {
	case r.uploadCapsMu <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-r.uploadCapsMu }()
	if r.uploadCaps != nil {
		return r.uploadCaps, nil
	}
	var err error
	r.uploadCaps, err = r.impl.uploadPackV2Capabilities(ctx)
	return r.uploadCaps, err
}

func parseObjectID(src []byte) (githash.SHA1, error) {
	var id githash.SHA1
	if err := id.UnmarshalText(src); err != nil {
		return githash.SHA1{}, fmt.Errorf("parse object id: %w", err)
	}
	return id, nil
}

type impl interface {
	uploadPackV2Capabilities(ctx context.Context) (v2Capabilities, error)
	uploadPackV2(ctx context.Context, cmd io.Reader) (io.ReadCloser, error)
	receivePack(ctx context.Context) (receivePackConn, error)
}

type receivePackConn interface {
	io.Reader
	io.Writer
	CloseWrite() error
	io.Closer
}

// v2Capabilities is the parsed result of an initial server query.
type v2Capabilities map[string]string

func (caps v2Capabilities) supports(key string) bool {
	_, ok := caps[key]
	return ok
}

func parseCapabilityAdvertisement(r *pktline.Reader) (v2Capabilities, error) {
	r.Next()
	if line, err := r.Text(); err != nil {
		return nil, fmt.Errorf("parse capability advertisement: %w", err)
	} else if !bytes.Equal(line, []byte("version 2")) {
		return nil, fmt.Errorf("parse capability advertisement: not Git protocol version 2 (%q)", line)
	}
	caps := make(v2Capabilities)
	for r.Next() && r.Type() != pktline.Flush {
		line, err := r.Text()
		if err != nil {
			return nil, fmt.Errorf("parse capability advertisement: %w", err)
		}
		// TODO(soon): Verify that key and value have permitted characters.
		k, v := line, []byte(nil)
		if i := bytes.IndexByte(line, '='); i != -1 {
			k, v = line[:i], line[i+1:]
		}
		caps[string(k)] = string(v)
	}
	if err := r.Err(); err != nil {
		return nil, fmt.Errorf("parse capability advertisement: %w", err)
	}
	return caps, nil
}

// ParseURL parses a Git remote URL, including the alternative SCP syntax.
// See git-fetch(1) for details.
func ParseURL(urlstr string) (*url.URL, error) {
	return giturl.Parse(urlstr)
}
