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

const v2ExtraParams = "version=2"

type fetchV2 struct {
	caps v2Capabilities
	impl impl
}

func (f *fetchV2) listRefs(ctx context.Context, refPrefixes []string) ([]*Ref, error) {
	if !f.caps.supports("ls-refs") {
		return nil, fmt.Errorf("unsupported by server")
	}

	var commandBuf []byte
	commandBuf = pktline.AppendString(commandBuf, "command=ls-refs\n")
	commandBuf = pktline.AppendDelim(commandBuf)
	commandBuf = pktline.AppendString(commandBuf, "symrefs\n")
	for _, prefix := range refPrefixes {
		commandBuf = pktline.AppendString(commandBuf, "ref-prefix "+prefix+"\n")
	}
	commandBuf = pktline.AppendFlush(commandBuf)
	resp, err := f.impl.uploadPack(ctx, v2ExtraParams, bytes.NewReader(commandBuf))
	if err != nil {
		return nil, err
	}
	defer resp.Close()
	var refs []*Ref
	respReader := pktline.NewReader(resp)
	for respReader.Next() && respReader.Type() != pktline.Flush {
		line, err := respReader.Text()
		if err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		words := bytes.Fields(line)
		if len(words) < 2 {
			return nil, fmt.Errorf("parse response: invalid packet from server")
		}
		ref := &Ref{Name: githash.Ref(words[1])}
		if !ref.Name.IsValid() {
			return nil, fmt.Errorf("parse response: ref %q: invalid name", ref.Name)
		}
		ref.ID, err = parseObjectID(words[0])
		if err != nil {
			return nil, fmt.Errorf("parse response: ref %s: %w", ref.Name, err)
		}
		for _, attr := range words[2:] {
			if val, ok := isRefAttribute(attr, "symref-target"); ok {
				ref.SymrefTarget = githash.Ref(val)
				if !ref.SymrefTarget.IsValid() {
					return nil, fmt.Errorf("parse response: ref %s: invalid symref target %q", ref.Name, ref.SymrefTarget)
				}
			}
		}
		refs = append(refs, ref)
	}
	if err := respReader.Err(); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return refs, nil
}

func isRefAttribute(b []byte, name string) (val []byte, ok bool) {
	if len(b) < len(name)+1 {
		return nil, false
	}
	for i := 0; i < len(name); i++ {
		if b[i] != name[i] {
			return nil, false
		}
	}
	if b[len(name)] != ':' {
		return nil, false
	}
	return b[len(name)+1:], true
}

func (f *fetchV2) fetch(ctx context.Context, req *FetchRequest) (_ io.ReadCloser, err error) {
	if !f.caps.supports("fetch") {
		return nil, fmt.Errorf("unsupported by server")
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
	resp, err := f.impl.uploadPack(ctx, v2ExtraParams, bytes.NewReader(commandBuf))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			resp.Close()
		}
	}()

	respReader := pktline.NewReader(resp)
	respReader.Next()
	line, err := respReader.Text()
	if err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if !bytes.Equal(line, []byte("packfile")) {
		return nil, fmt.Errorf("parse response: unknown section %q", line)
	}
	return &v2Reader{
		packReader: respReader,
		packCloser: resp,
		progress:   req.Progress,
	}, nil
}

type v2Reader struct {
	packReader *pktline.Reader
	packCloser io.Closer

	curr      []byte // current packfile packet
	packError error
	progress  io.Writer
}

func (f *v2Reader) Read(p []byte) (int, error) {
	if len(f.curr) > 0 {
		n := copy(p, f.curr)
		f.curr = f.curr[n:]
		return n, nil
	}
	if f.packError != nil {
		return 0, f.packError
	}
	n, err := f.read(p)
	if err != nil {
		f.packError = err
	}
	return n, err
}

func (f *v2Reader) read(p []byte) (int, error) {
	for f.packReader.Next() && f.packReader.Type() == pktline.Data {
		pkt, err := f.packReader.Bytes()
		if err != nil {
			return 0, err
		}
		if len(pkt) == 0 {
			return 0, fmt.Errorf("empty packet")
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
			return 0, fmt.Errorf("server error: %s", trimLF(data))
		default:
			return 0, fmt.Errorf("encountered bad stream code (%02x)", pktType)
		}
	}
	if err := f.packReader.Err(); err != nil {
		return 0, err
	}
	return 0, io.EOF
}

func trimLF(line []byte) []byte {
	if len(line) == 0 || line[len(line)-1] != '\n' {
		return line
	}
	return line[:len(line)-1]
}

func (f *v2Reader) Close() error {
	return f.packCloser.Close()
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
