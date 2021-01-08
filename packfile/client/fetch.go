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

	hash "gg-scm.io/pkg/git/githash"
)

// FetchOptions holds the optional arguments to Fetch.
type FetchOptions struct {
	Have       []hash.SHA1
	IncludeTag bool

	// OnProgress is called when a progress message is received.
	OnProgress func(string)
}

// Fetch starts a stream of packfile data from the remote.
func (r *Remote) Fetch(ctx context.Context, dst io.Writer, want hash.SHA1, opts *FetchOptions) error {
	caps, err := r.ensureUploadCaps(ctx)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", r.urlstr, err)
	}
	if !caps.supports("fetch") {
		return fmt.Errorf("fetch %s: unsupported by server", r.urlstr)
	}
	if opts == nil {
		opts = new(FetchOptions)
	}

	var commandBuf []byte
	commandBuf = appendPacketLineString(commandBuf, "command=fetch\n")
	commandBuf = appendDelimPacket(commandBuf)
	commandBuf = appendPacketLineString(commandBuf, "want "+want.String()+"\n")
	for _, have := range opts.Have {
		commandBuf = appendPacketLineString(commandBuf, "have "+have.String()+"\n")
	}
	// TODO(soon): Add commit negotiation option.
	commandBuf = appendPacketLineString(commandBuf, "done\n")
	if opts.IncludeTag {
		commandBuf = appendPacketLineString(commandBuf, "include-tag\n")
	}
	if opts.OnProgress == nil {
		commandBuf = appendPacketLineString(commandBuf, "no-progress\n")
	}
	commandBuf = appendFlushPacket(commandBuf)
	resp, err := r.impl.uploadPackV2(ctx, bytes.NewReader(commandBuf))
	if err != nil {
		return fmt.Errorf("fetch %s: %w", r.urlstr, err)
	}
	defer resp.Close()

	// packfile section
	pbuf := make([]byte, 100)
	ptype, n, err := readPacketLine(resp, pbuf)
	if err != nil {
		return fmt.Errorf("fetch %s: parse response: %w", r.urlstr, err)
	}
	if ptype != dataPacket {
		return fmt.Errorf("fetch %s: parse response: unexpected flush", r.urlstr)
	}
	if got := string(trimLF(pbuf[:n])); got != "packfile" {
		return fmt.Errorf("fetch %s: parse response: unknown section %q", r.urlstr, got)
	}
	if err := handlePackfileSection(dst, opts.OnProgress, resp); err != nil {
		return fmt.Errorf("fetch %s: %w", r.urlstr, err)
	}
	return nil
}

func handlePackfileSection(dst io.Writer, onProgress func(string), src io.Reader) error {
	buf := make([]byte, maxPacketSize)
	for {
		ptype, n, err := readPacketLine(src, buf)
		if ptype == flushPacket || ptype == delimPacket {
			return nil
		}
		if err != nil {
			return fmt.Errorf("packfile section: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("packfile section: empty packet")
		}
		got := buf[1:n]
		switch buf[0] {
		case 1:
			// Pack data
			if _, err := dst.Write(got); err != nil {
				return fmt.Errorf("packfile section: %w", err)
			}
		case 2:
			// Progress message
			if onProgress != nil {
				onProgress(string(trimLF(got)))
			}
		case 3:
			// Fatal error message
			return fmt.Errorf("packfile section: server error: %s", got)
		default:
			return fmt.Errorf("packfile section: encountered bad stream code (%02x)", buf[0])
		}
	}
}
