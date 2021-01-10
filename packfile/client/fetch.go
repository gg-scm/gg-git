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
	"context"
	"fmt"
	"io"

	"gg-scm.io/pkg/git/githash"
	"gg-scm.io/pkg/git/internal/pktline"
)

// FetchStream represents a git-upload-pack session.
type FetchStream struct {
	ctx      context.Context
	urlstr   string
	impl     fetcher
	packfile io.ReadCloser
}

type fetcher interface {
	fetch(ctx context.Context, req *FetchRequest) (io.ReadCloser, error)
	listRefs(ctx context.Context, refPrefixes []string) ([]*Ref, error)
}

// StartFetch starts a git-upload-pack session on the remote.
// The Context is used for the entire fetch stream. The caller is responsible
// for calling Close on the returned FetchStream.
func (r *Remote) StartFetch(ctx context.Context) (*FetchStream, error) {
	resp, err := r.impl.advertiseRefs(ctx, v2ExtraParams)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", r.urlstr, err)
	}
	defer resp.Close()
	caps, err := parseCapabilityAdvertisement(pktline.NewReader(resp))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", r.urlstr, err)
	}
	return &FetchStream{
		ctx:    ctx,
		urlstr: r.urlstr,
		impl: &fetchV2{
			caps: caps,
			impl: r.impl,
		},
	}, nil
}

// Ref describes a single reference to a Git object.
type Ref struct {
	ID           githash.SHA1
	Name         githash.Ref
	SymrefTarget githash.Ref
}

// ListRefs lists the remote's references. If refPrefixes is given, then only
// refs that start with one of the given strings are returned.
func (f *FetchStream) ListRefs(refPrefixes ...string) ([]*Ref, error) {
	refs, err := f.impl.listRefs(f.ctx, refPrefixes)
	if err != nil {
		return nil, fmt.Errorf("list refs for %s: %w", f.urlstr, err)
	}
	return refs, nil
}

// A FetchRequest informs the remote which objects to include in the packfile.
type FetchRequest struct {
	// Want is the set of object IDs to send. At least one must be specified,
	// or SendRequest will return an error.
	Want []githash.SHA1
	// Have is the set of object IDs that the server can exclude from the
	// packfile. It may be empty.
	Have []githash.SHA1
	// IncludeTag indicates whether annotated tags should be sent if the objects
	// they point to are being sent.
	IncludeTag bool
	// Progress will receive progress messages from the remote while the caller
	// reads the packfile. It may be nil.
	Progress io.Writer
}

// SendRequest requests a packfile from the remote. It must be called before
// calling the Read method.
func (f *FetchStream) SendRequest(req *FetchRequest) (err error) {
	if len(req.Want) == 0 {
		return fmt.Errorf("fetch %s: no objects requested", f.urlstr)
	}
	if f.packfile != nil {
		return fmt.Errorf("fetch %s: send request: request already sent", f.urlstr)
	}
	resp, err := f.impl.fetch(f.ctx, req)
	if err != nil {
		return fmt.Errorf("fetch %s: send request: %w", f.urlstr, err)
	}
	f.packfile = resp
	return nil
}

// Read reads the packfile stream, returning the number of bytes read into p and
// any error that occurred. It is an error to call Read before calling SendRequest.
//
// Read will write any progress messages sent by the remote to the Progress
// writer specified in SendRequest.
func (f *FetchStream) Read(p []byte) (int, error) {
	if f.packfile == nil {
		return 0, fmt.Errorf("fetch %s: read packfile: read attempted before request", f.urlstr)
	}
	n, err := f.packfile.Read(p)
	if err == io.EOF {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("fetch %s: read packfile: %w", f.urlstr, err)
	}
	return n, nil
}

// Close releases any resources used by the stream.
func (f *FetchStream) Close() error {
	if f.packfile != nil {
		return f.packfile.Close()
	}
	return nil
}
