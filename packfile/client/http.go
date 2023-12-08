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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"gg-scm.io/pkg/git/internal/pktline"
)

const (
	authorizationHeader = "Authorization"
	contentTypeHeader   = "Content-Type"
	gitProtocolHeader   = "Git-Protocol"
	userAgentHeader     = "User-Agent"
)

type httpRemote struct {
	client        *http.Client
	base          *url.URL
	authorization string
	userAgent     string
}

func (r *httpRemote) do(ctx context.Context, req *http.Request) (*http.Response, error) {
	req = req.Clone(ctx)
	req.Header = r.fillHeaders(req.Header)
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("http %s", resp.Status)
	}
	return resp, nil
}

func (r *httpRemote) url(path string, params url.Values) *url.URL {
	u := new(url.URL)
	*u = *r.base
	u.Path += path
	q := u.Query()
	for k, v := range params {
		q[k] = v
	}
	u.RawQuery = q.Encode()
	return u
}

func (r *httpRemote) fillHeaders(h http.Header) http.Header {
	if h == nil {
		h = make(http.Header)
	}
	if _, set := h[userAgentHeader]; !set && r.userAgent != "" {
		h.Set(userAgentHeader, r.userAgent)
	}
	if _, set := h[authorizationHeader]; !set && r.authorization != "" {
		h.Set(authorizationHeader, r.authorization)
	}
	return h
}

func (r *httpRemote) advertiseRefs(ctx context.Context, extraParams string) (_ io.ReadCloser, err error) {
	resp, err := r.do(ctx, &http.Request{
		Method: http.MethodGet,
		URL:    r.url("/info/refs", url.Values{"service": {"git-upload-pack"}}),
		Header: http.Header{
			gitProtocolHeader: {extraParams},
		},
	})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			resp.Body.Close()
		}
	}()
	if contentType := resp.Header.Get("Content-Type"); contentType != "application/x-git-upload-pack-advertisement" {
		return nil, fmt.Errorf("content-type is %q, not git upload pack", contentType)
	}
	buf := new(bytes.Buffer) // in case we need to replay. See below.
	respReader := pktline.NewReader(io.TeeReader(resp.Body, buf))
	const want = "# service=git-upload-pack"
	respReader.Next()
	line, err := respReader.Text()
	if err != nil {
		return nil, fmt.Errorf("initial packet: %w", err)
	}
	if !bytes.Equal(line, []byte(want)) {
		// For version 2, git-http-backend 2.20.1 will not send the opening line,
		// but GitHub will. The documentation is ambiguous as to whether this line
		// should be included.
		// Discussion upstream: https://public-inbox.org/git/CAEs=z9Pajgjnq56+umA+g9-NFv-Rzo9m5sa-7cow_byckLiJ0A@mail.gmail.com/
		//
		// Trying to balance this with the http-protocol guidance that "clients MUST
		// verify the first pkt-line is # service=$servicename", we permit this if
		// we're sending V2.
		if specifiesVersion2(extraParams) {
			return readCloserCombiner{io.MultiReader(buf, resp.Body), resp.Body}, nil
		}
		return nil, fmt.Errorf("invalid initial packet")
	}
	if !respReader.Next() {
		return nil, respReader.Err()
	}
	if respReader.Type() != pktline.Flush {
		return nil, fmt.Errorf("invalid initial packet")
	}
	return resp.Body, nil
}

func (r *httpRemote) uploadPack(ctx context.Context, extraParams string, request io.Reader) (_ io.ReadCloser, err error) {
	resp, err := r.do(ctx, &http.Request{
		Method: http.MethodPost,
		URL:    r.url("/git-upload-pack", nil),
		Header: http.Header{
			contentTypeHeader: {"application/x-git-upload-pack-request"},
			gitProtocolHeader: {extraParams},
		},
		Body: io.NopCloser(request),
	})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			resp.Body.Close()
		}
	}()
	if contentType := resp.Header.Get("Content-Type"); contentType != "application/x-git-upload-pack-result" {
		return nil, fmt.Errorf("content-type is %q, not git upload pack", contentType)
	}
	return resp.Body, nil
}

func (r *httpRemote) receivePack(ctx context.Context) (_ receivePackConn, err error) {
	resp, err := r.do(ctx, &http.Request{
		Method: http.MethodGet,
		URL:    r.url("/info/refs", url.Values{"service": {"git-receive-pack"}}),
	})
	if err != nil {
		return nil, fmt.Errorf("receive-pack: refs: %w", err)
	}
	defer func() {
		if err != nil {
			resp.Body.Close()
		}
	}()
	if contentType := resp.Header.Get("Content-Type"); contentType != "application/x-git-receive-pack-advertisement" {
		return nil, fmt.Errorf("receive-pack: refs: content-type is %q, not git receive pack", contentType)
	}
	respReader := pktline.NewReader(resp.Body)
	const want = "# service=git-receive-pack"
	respReader.Next()
	if line, err := respReader.Text(); err != nil {
		return nil, fmt.Errorf("receive-pack: refs: %w", err)
	} else if !bytes.Equal(line, []byte(want)) {
		return nil, fmt.Errorf("receive-pack: refs: invalid initial packet")
	}
	if !respReader.Next() {
		return nil, fmt.Errorf("receive-pack: refs: %w", respReader.Err())
	}
	if respReader.Type() != pktline.Flush {
		return nil, fmt.Errorf("receive-pack: refs: invalid initial packet")
	}
	return &httpReceivePackConn{
		ctx:          ctx,
		remote:       r,
		refs:         resp.Body,
		respReceived: make(chan struct{}),
	}, nil
}

type httpReceivePackConn struct {
	ctx    context.Context
	remote *httpRemote

	refs      io.ReadCloser
	refsEnded bool // true if we saw EOF from refs

	w *io.PipeWriter // git-receive-pack request body. nil until request starts.

	respReceived chan struct{} // closed when git-receive-pack responds
	respBody     io.ReadCloser
	respError    error
}

func (conn *httpReceivePackConn) Read(buf []byte) (int, error) {
	if !conn.refsEnded {
		return conn.readRefs(buf)
	}
	return conn.readResponse(buf)
}

func (conn *httpReceivePackConn) readRefs(buf []byte) (int, error) {
	n, err := conn.refs.Read(buf)
	if errors.Is(err, io.EOF) {
		conn.refsEnded = true
		if n == 0 {
			return conn.readResponse(buf)
		}
		return n, nil
	}
	if err != nil {
		err = fmt.Errorf("receive-pack: read refs: %w", err)
	}
	return n, err
}

func (conn *httpReceivePackConn) readResponse(buf []byte) (int, error) {
	select {
	case <-conn.respReceived:
	case <-conn.ctx.Done():
		return 0, fmt.Errorf("receive-pack: read response: %w", conn.ctx.Err())
	}
	if errors.Is(conn.respError, io.EOF) {
		return 0, conn.respError
	}
	if conn.respError != nil {
		return 0, fmt.Errorf("receive-pack: read response: %w", conn.respError)
	}

	n, err := conn.respBody.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		err = fmt.Errorf("receive-pack: read response: %w", err)
	}
	return n, err
}

func (conn *httpReceivePackConn) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	if conn.w == nil {
		// On first write, kick off a request to POST /git-receive-pack
		conn.start()
	}
	n, err := conn.w.Write(data)
	if err != nil {
		err = fmt.Errorf("receive-pack: %w", err)
	}
	return n, err
}

func (conn *httpReceivePackConn) CloseWrite() error {
	if conn.w == nil {
		return nil
	}
	return conn.w.Close()
}

func (conn *httpReceivePackConn) start() {
	var pr *io.PipeReader
	pr, conn.w = io.Pipe()

	go func() {
		defer close(conn.respReceived)
		resp, err := conn.remote.do(conn.ctx, &http.Request{
			Method: http.MethodPost,
			URL:    conn.remote.url("/git-receive-pack", nil),
			Body:   pr,
			Header: http.Header{
				"Content-Type": {"application/x-git-receive-pack-request"},
				"Expect":       {"100-continue"},
			},
		})
		if err != nil {
			conn.respError = err
			return
		}
		defer func() {
			// Close the response body if we end up not using it.
			if conn.respBody == nil {
				resp.Body.Close()
			}
		}()
		if resp.StatusCode != http.StatusOK {
			conn.respError = fmt.Errorf("http %s", resp.Status)
			return
		}
		const wantContentType = "application/x-git-receive-pack-result"
		if got := resp.Header.Get(contentTypeHeader); got != wantContentType {
			conn.respError = fmt.Errorf("content-type is %q (expected %s)", got, wantContentType)
			return
		}
		conn.respBody = resp.Body
	}()
}

func (conn *httpReceivePackConn) Close() error {
	conn.refs.Close() // Just reads, so error unimportant.
	if conn.w == nil {
		// Set response error to io.EOF in case there's a blocked Read.
		conn.respError = io.EOF
		close(conn.respReceived)
		return nil
	}
	// Close the request body first.
	conn.w.Close()
	// Wait for HTTP response.
	<-conn.respReceived
	if conn.respError != nil {
		return fmt.Errorf("receive-pack: %w", conn.respError)
	}
	return conn.respBody.Close()
}

func specifiesVersion2(extraParams string) bool {
	for _, param := range strings.Split(extraParams, ":") {
		if param == v2ExtraParams {
			return true
		}
	}
	return false
}

type readCloserCombiner struct {
	io.Reader
	io.Closer
}
