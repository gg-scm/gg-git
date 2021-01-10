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
	"fmt"
	"io"

	"gg-scm.io/pkg/git/githash"
	"gg-scm.io/pkg/git/internal/pktline"
)

// FetchStream represents a git-upload-pack session.
type FetchStream struct {
	ctx    context.Context
	urlstr string
	impl   impl
	caps   v2Capabilities

	packReader *pktline.Reader
	packCloser io.Closer
	packError  error
	curr       []byte // current packfile packet
	progress   io.Writer
}

// StartFetch starts a git-upload-pack session on the remote.
// The Context is used for the entire fetch stream. The caller is responsible
// for calling Close on the returned FetchStream.
func (r *Remote) StartFetch(ctx context.Context) (*FetchStream, error) {
	caps, err := r.ensureUploadCaps(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", r.urlstr, err)
	}
	return &FetchStream{
		ctx:    ctx,
		urlstr: r.urlstr,
		impl:   r.impl,
		caps:   caps,
	}, nil
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
	if !f.caps.supports("fetch") {
		return fmt.Errorf("fetch %s: unsupported by server", f.urlstr)
	}

	var commandBuf []byte
	commandBuf = pktline.AppendString(commandBuf, "command=fetch\n")
	commandBuf = pktline.AppendDelim(commandBuf)
	for _, want := range req.Want {
		commandBuf = pktline.AppendString(commandBuf, "want "+want.String()+"\n")
	}
	for _, have := range req.Have {
		commandBuf = pktline.AppendString(commandBuf, "have "+have.String()+"\n")
	}
	// TODO(soon): Add commit negotiation option.
	commandBuf = pktline.AppendString(commandBuf, "done\n")
	if req.IncludeTag {
		commandBuf = pktline.AppendString(commandBuf, "include-tag\n")
	}
	if req.Progress == nil {
		commandBuf = pktline.AppendString(commandBuf, "no-progress\n")
	}
	commandBuf = pktline.AppendFlush(commandBuf)
	resp, err := f.impl.uploadPackV2(f.ctx, bytes.NewReader(commandBuf))
	if err != nil {
		return fmt.Errorf("fetch %s: %w", f.urlstr, err)
	}
	f.packReader = pktline.NewReader(resp)
	f.packCloser = resp
	f.packError = nil
	f.curr = nil
	f.progress = nil
	defer func() {
		if err != nil {
			f.packReader = nil
			f.packCloser = nil
			resp.Close()
		}
	}()

	f.packReader.Next()
	line, err := f.packReader.Text()
	if err != nil {
		return fmt.Errorf("fetch %s: parse response: %w", f.urlstr, err)
	}
	if !bytes.Equal(line, []byte("packfile")) {
		return fmt.Errorf("fetch %s: parse response: unknown section %q", f.urlstr, line)
	}
	return nil
}

// Read reads the packfile stream, returning the number of bytes read into p and
// any error that occurred. It is an error to call Read before calling SendRequest.
//
// Read will write any progress messages sent by the remote to the Progress
// writer specified in SendRequest.
func (f *FetchStream) Read(p []byte) (int, error) {
	if f.packError != nil {
		return 0, f.packError
	}
	if f.packReader == nil {
		return 0, fmt.Errorf("fetch %s: read packfile: read attempted before request", f.urlstr)
	}
	if len(f.curr) > 0 {
		n := copy(p, f.curr)
		f.curr = f.curr[n:]
		return n, nil
	}
	n, err := f.read(p)
	if err != nil {
		f.packError = err
	}
	return n, err
}

func (f *FetchStream) read(p []byte) (int, error) {
	for f.packReader.Next() && f.packReader.Type() == pktline.Data {
		pkt, err := f.packReader.Bytes()
		if err != nil {
			return 0, fmt.Errorf("fetch %s: read packfile: %w", f.urlstr, err)
		}
		if len(pkt) == 0 {
			return 0, fmt.Errorf("fetch %s: read packfile: empty packet", f.urlstr)
		}
		pktType, data := pkt[0], pkt[1:]
		switch pktType {
		case 1:
			// Pack data
			n := copy(p, data)
			f.curr = data[n:]
			return n, nil
		case 2:
			// Progress message
			if f.progress != nil {
				f.progress.Write(data)
			}
		case 3:
			// Fatal error message
			return 0, fmt.Errorf("fetch %s: read packfile: server error: %s", f.urlstr, trimLF(data))
		default:
			return 0, fmt.Errorf("fetch %s: read packfile: encountered bad stream code (%02x)", f.urlstr, pktType)
		}
	}
	if err := f.packReader.Err(); err != nil {
		return 0, fmt.Errorf("fetch %s: read packfile: %w", f.urlstr, err)
	}
	return 0, io.EOF
}

func trimLF(line []byte) []byte {
	if len(line) == 0 || line[len(line)-1] != '\n' {
		return line
	}
	return line[:len(line)-1]
}

// Close releases any resources used by the stream.
func (f *FetchStream) Close() error {
	if f.packCloser != nil {
		// TODO(someday): Close error is usually signaled.
		f.packCloser.Close()
	}
	return nil
}
