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

	"gg-scm.io/pkg/git/githash"
	"gg-scm.io/pkg/git/internal/pktline"
)

// Reference: https://git-scm.com/docs/protocol-v2

const v2ExtraParams = "version=2"

type fetchV2 struct {
	caps capabilityList
	impl impl
}

func (f *fetchV2) Close() error {
	return nil
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

const (
	ackPrefix = "ACK "
	nak       = "NAK"
)

func (f *fetchV2) negotiate(ctx context.Context, errPrefix string, req *FetchRequest) (_ *FetchResponse, err error) {
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
	if !req.HaveMore {
		commandBuf = pktline.AppendString(commandBuf, "done\n")
	}
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
	result := &FetchResponse{
		Packfile: &packfileReader{
			errPrefix:  errPrefix,
			packReader: respReader,
			packCloser: resp,
			progress:   req.Progress,
		},
	}
	const packfileSection = "packfile"
	switch string(line) {
	case packfileSection:
		return result, nil
	case "acknowledgements":
		result.Acks = make(map[githash.SHA1]bool)
		ready := false
		for respReader.Next() && respReader.Type() == pktline.Data {
			line, err := respReader.Text()
			if err != nil {
				return nil, fmt.Errorf("parse response: acknowledgements: %w", err)
			}
			switch {
			case bytes.HasPrefix(line, []byte(ackPrefix)):
				var id githash.SHA1
				if err := id.UnmarshalText(line[len(ackPrefix):]); err != nil {
					return nil, fmt.Errorf("parse response: acknowledgements: %w", err)
				}
				result.Acks[id] = true
			case bytes.Equal(line, []byte(nak)):
			case bytes.Equal(line, []byte("ready")):
				ready = true
			default:
				return nil, fmt.Errorf("parse response: acknowledgements: unrecognized directive %q", line)
			}
		}
		if err := respReader.Err(); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		if !ready {
			result.Packfile = nil
			resp.Close()
			return result, nil
		}
		if respReader.Type() != pktline.Delim {
			return nil, fmt.Errorf("parse response: acknowledgements: expected delim or data (got %v)", respReader.Type())
		}

		respReader.Next()
		line, err = respReader.Text()
		if err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		if !bytes.Equal(line, []byte(packfileSection)) {
			return nil, fmt.Errorf("parse response: unexpected section %q", line)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("parse response: unknown section %q", line)
	}
}

const version2Line = "version 2"

// parseCapabilityAdvertisementV2 parses the version 2 "refs" advertisement
// response. The caller is expected to have advanced r to the "version 2" line
// before calling parseCapabilityAdvertisementV2.
func parseCapabilityAdvertisementV2(r *pktline.Reader) (capabilityList, error) {
	if line, err := r.Text(); err != nil {
		return nil, fmt.Errorf("parse capability advertisement: %w", err)
	} else if !bytes.Equal(line, []byte(version2Line)) {
		return nil, fmt.Errorf("parse capability advertisement: not Git protocol version 2 (%q)", line)
	}
	caps := make(capabilityList)
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
