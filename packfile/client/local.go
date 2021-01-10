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
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"gg-scm.io/pkg/git/internal/pktline"
	"gg-scm.io/pkg/git/internal/sigterm"
)

type fileRemote struct {
	uploadPackPath  string
	receivePackPath string
	dir             string
}

func (r *fileRemote) advertiseRefs(ctx context.Context, extraParams string) (io.ReadCloser, error) {
	return r.startUploadPack(ctx, "--advertise-refs", extraParams, nil)
}

func (r *fileRemote) uploadPack(ctx context.Context, extraParams string, request io.Reader) (io.ReadCloser, error) {
	return r.startUploadPack(ctx, "--stateless-rpc", extraParams, request)
}

func (r *fileRemote) startUploadPack(ctx context.Context, mode string, extraParams string, request io.Reader) (*uploadPackReader, error) {
	errPrefix := "git-upload-pack " + mode + " " + r.dir
	c := exec.Command(r.uploadPackPath, mode, "--", r.dir)
	c.Env = append(os.Environ(), "GIT_PROTOCOL="+extraParams)
	c.Stdin = request
	stdout, err := c.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	// TODO(now): remove (debugging)
	c.Stderr = os.Stderr
	wait, err := sigterm.Start(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	return &uploadPackReader{
		errPrefix: errPrefix,
		pipe:      stdout,
		wait:      wait,
	}, nil
}

type uploadPackReader struct {
	errPrefix string
	pipe      io.ReadCloser
	wait      func() error
}

func (r *uploadPackReader) Read(p []byte) (int, error) {
	return r.pipe.Read(p)
}

func (r *uploadPackReader) WriteTo(w io.Writer) (int64, error) {
	return io.Copy(w, r.pipe)
}

func (r *uploadPackReader) Close() error {
	r.pipe.Close()
	if err := r.wait(); err != nil {
		if exit := (*exec.ExitError)(nil); errors.As(err, &exit) && !exit.Exited() {
			// Signaled.
			return nil
		}
		return fmt.Errorf("%s: %w", r.errPrefix, err)
	}
	return nil
}

func (r *fileRemote) receivePack(ctx context.Context) (receivePackConn, error) {
	errPrefix := "git-receive-pack " + r.dir
	c := exec.Command(r.receivePackPath, "--", r.dir)
	stdin, err := c.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	wait, err := sigterm.Start(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	return &localReceivePackConn{
		errPrefix: errPrefix,
		stdin:     stdin,
		stdout:    stdout,
		wait:      wait,
	}, nil
}

type localReceivePackConn struct {
	errPrefix string
	stdin     io.WriteCloser
	stdout    io.Reader
	wait      func() error
	wrote     bool
}

func (conn *localReceivePackConn) Read(p []byte) (int, error) {
	n, err := conn.stdout.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		err = fmt.Errorf("%s: %w", conn.errPrefix, err)
	}
	return n, err
}

func (conn *localReceivePackConn) Write(p []byte) (int, error) {
	n, err := conn.stdin.Write(p)
	if err != nil {
		err = fmt.Errorf("%s: %w", conn.errPrefix, err)
	}
	if n > 0 {
		conn.wrote = true
	}
	return n, err
}

func (conn *localReceivePackConn) CloseWrite() error {
	return conn.stdin.Close()
}

func (conn *localReceivePackConn) Close() error {
	if !conn.wrote {
		conn.stdin.Write(pktline.AppendFlush(nil))
	}
	conn.stdin.Close()
	if err := conn.wait(); err != nil {
		return fmt.Errorf("%s: %w", conn.errPrefix, err)
	}
	return nil
}
