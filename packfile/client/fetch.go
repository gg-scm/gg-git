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
	"gg-scm.io/pkg/git/internal/pktline"
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
	commandBuf = pktline.AppendString(commandBuf, "command=fetch\n")
	commandBuf = pktline.AppendDelim(commandBuf)
	commandBuf = pktline.AppendString(commandBuf, "want "+want.String()+"\n")
	for _, have := range opts.Have {
		commandBuf = pktline.AppendString(commandBuf, "have "+have.String()+"\n")
	}
	// TODO(soon): Add commit negotiation option.
	commandBuf = pktline.AppendString(commandBuf, "done\n")
	if opts.IncludeTag {
		commandBuf = pktline.AppendString(commandBuf, "include-tag\n")
	}
	if opts.OnProgress == nil {
		commandBuf = pktline.AppendString(commandBuf, "no-progress\n")
	}
	commandBuf = pktline.AppendFlush(commandBuf)
	resp, err := r.impl.uploadPackV2(ctx, bytes.NewReader(commandBuf))
	if err != nil {
		return fmt.Errorf("fetch %s: %w", r.urlstr, err)
	}
	defer resp.Close()

	// packfile section
	respReader := pktline.NewReader(resp)
	respReader.Next()
	if line, err := respReader.Text(); err != nil {
		return fmt.Errorf("fetch %s: parse response: %w", r.urlstr, err)
	} else if !bytes.Equal(line, []byte("packfile")) {
		return fmt.Errorf("fetch %s: parse response: unknown section %q", r.urlstr, line)
	}
	if err := handlePackfileSection(dst, opts.OnProgress, respReader); err != nil {
		return fmt.Errorf("fetch %s: %w", r.urlstr, err)
	}
	return nil
}

func handlePackfileSection(dst io.Writer, onProgress func(string), src *pktline.Reader) error {
	for src.Next() && src.Type() == pktline.Data {
		pkt, err := src.Bytes()
		if err != nil {
			return err
		}
		if err != nil {
			return fmt.Errorf("packfile section: %w", err)
		}
		if len(pkt) == 0 {
			return fmt.Errorf("packfile section: empty packet")
		}
		pktType, data := pkt[0], pkt[1:]
		switch pktType {
		case 1:
			// Pack data
			if _, err := dst.Write(data); err != nil {
				return fmt.Errorf("packfile section: %w", err)
			}
		case 2:
			// Progress message
			if onProgress != nil {
				onProgress(string(trimLF(data)))
			}
		case 3:
			// Fatal error message
			return fmt.Errorf("packfile section: server error: %s", trimLF(data))
		default:
			return fmt.Errorf("packfile section: encountered bad stream code (%02x)", pktType)
		}
	}
	if err := src.Err(); err != nil {
		return fmt.Errorf("packfile section: %w", err)
	}
	return nil
}

func trimLF(line []byte) []byte {
	if len(line) == 0 || line[len(line)-1] != '\n' {
		return line
	}
	return line[:len(line)-1]
}
