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
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
)

type fileRemote struct {
	uploadPackPath  string
	receivePackPath string
	dir             string
}

func (r *fileRemote) uploadPackCapabilities(ctx context.Context) (v2Capabilities, error) {
	c := exec.Command(r.uploadPackPath, "--advertise-refs", "--", r.dir)
	c.Env = append(os.Environ(), "GIT_PROTOCOL=version=2")
	out := new(bytes.Buffer)
	c.Stdout = out
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("git-upload-pack --advertise-refs %s: %w", r.dir, err)
	}
	waited := make(chan struct{})
	killGoroutineDone := make(chan struct{})
	go func() {
		defer close(killGoroutineDone)
		select {
		case <-ctx.Done():
			c.Process.Signal(unix.SIGTERM)
		case <-waited:
		}
	}()
	err := c.Wait()
	close(waited)
	<-killGoroutineDone
	if err != nil {
		return nil, fmt.Errorf("git-upload-pack --advertise-refs %s: %w", r.dir, err)
	}
	caps, err := parseCapabilityAdvertisement(out)
	if err != nil {
		return nil, fmt.Errorf("git-upload-pack --advertise-refs %s: %w", r.dir, err)
	}
	return caps, nil
}

func (r *fileRemote) uploadPack(ctx context.Context, cmd io.Reader) (io.ReadCloser, error) {
	c := exec.Command(r.uploadPackPath, "--stateless-rpc", "--", r.dir)
	c.Env = append(os.Environ(), "GIT_PROTOCOL=version=2")
	c.Stdin = cmd
	stdout, err := c.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("git-upload-pack --stateless-rpc %s: %w", r.dir, err)
	}
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("git-upload-pack --stateless-rpc %s: %w", r.dir, err)
	}
	return &uploadPackReader{
		dir:  r.dir,
		c:    c,
		pipe: stdout,
	}, nil
}

type uploadPackReader struct {
	dir  string
	c    *exec.Cmd
	pipe io.ReadCloser
}

func (r *uploadPackReader) Read(p []byte) (int, error) {
	return r.pipe.Read(p)
}

func (r *uploadPackReader) Close() error {
	r.c.Process.Signal(unix.SIGTERM)
	r.pipe.Close()
	if err := r.c.Wait(); err != nil {
		return fmt.Errorf("git-upload-pack --stateless-rpc %s: %w", r.dir, err)
	}
	return nil
}

func (r *fileRemote) receivePack(ctx context.Context) (receivePackConn, error) {
	c := exec.Command(r.receivePackPath, "--", r.dir)
	stdin, err := c.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("git-receive-pack %s: %w", r.dir, err)
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("git-receive-pack %s: %w", r.dir, err)
	}
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("git-receive-pack %s: %w", r.dir, err)
	}
	return &localReceivePackConn{
		dir:    r.dir,
		c:      c,
		stdin:  stdin,
		stdout: stdout,
	}, nil
}

type localReceivePackConn struct {
	dir    string
	c      *exec.Cmd
	stdin  io.WriteCloser
	stdout io.Reader
	wrote  bool
}

func (conn *localReceivePackConn) Read(p []byte) (int, error) {
	n, err := conn.stdout.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		err = fmt.Errorf("git-receive-pack %s: %w", conn.dir, err)
	}
	return n, err
}

func (conn *localReceivePackConn) Write(p []byte) (int, error) {
	n, err := conn.stdin.Write(p)
	if err != nil {
		err = fmt.Errorf("git-receive-pack %s: %w", conn.dir, err)
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
		conn.stdin.Write(appendFlushPacket(nil))
	}
	conn.stdin.Close()
	if err := conn.c.Wait(); err != nil {
		return fmt.Errorf("git-receive-pack %s: %w", conn.dir, err)
	}
	return nil
}
