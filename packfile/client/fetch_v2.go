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
	"strings"

	"gg-scm.io/pkg/git/githash"
	"gg-scm.io/pkg/git/internal/pktline"
)

// Reference: https://git-scm.com/docs/protocol-v2

const v2ExtraParams = "version=2"

const (
	listRefsV2Command = "ls-refs"
	fetchV2Command    = "fetch"
)

type fetchV2 struct {
	caps capabilityList
	impl impl
}

func (f *fetchV2) Close() error {
	return nil
}

func (f *fetchV2) listRefs(ctx context.Context, refPrefixes []string) ([]*Ref, error) {
	if !f.caps.supports(listRefsV2Command) {
		return nil, fmt.Errorf("unsupported by server")
	}

	var commandBuf []byte
	commandBuf = pktline.AppendString(commandBuf, "command="+listRefsV2Command+"\n")
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
		ref.ObjectID, err = parseObjectID(words[0])
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

func (f *fetchV2) capabilities() FetchCapabilities {
	caps := FetchCapIncludeTag | FetchCapThinPack
	for _, feature := range strings.Fields(f.caps[fetchV2Command]) {
		switch feature {
		case shallowCap:
			caps |= FetchCapShallow | FetchCapDepthRelative | FetchCapSince | FetchCapShallowExclude
		case filterCap:
			caps |= FetchCapFilter
		}
	}
	return caps
}

func (f *fetchV2) negotiate(ctx context.Context, errPrefix string, req *FetchRequest) (_ *FetchResponse, err error) {
	if !f.caps.supports(fetchV2Command) {
		return nil, fmt.Errorf("unsupported by server")
	}
	commandBuf := formatFetchRequestV2(req)
	resp, err := f.impl.uploadPack(ctx, v2ExtraParams, bytes.NewReader(commandBuf))
	if err != nil {
		return nil, err
	}
	respReader := pktline.NewReader(resp)
	result, err := readFetchOutputV2(respReader, func(_ *pktline.Reader) io.ReadCloser {
		return &packfileReader{
			errPrefix:  errPrefix,
			packReader: respReader,
			packCloser: resp,
			progress:   req.Progress,
		}
	})
	if err != nil || result.Packfile == nil {
		resp.Close()
	}
	return result, err
}

func formatFetchRequestV2(req *FetchRequest) []byte {
	var buf []byte
	buf = pktline.AppendString(buf, "command="+fetchV2Command+"\n")
	buf = pktline.AppendDelim(buf)
	for _, want := range req.Want {
		buf = pktline.AppendString(buf, "want "+want.String()+"\n")
	}
	for _, have := range req.Have {
		buf = pktline.AppendString(buf, "have "+have.String()+"\n")
	}
	if !req.HaveMore {
		buf = pktline.AppendString(buf, "done\n")
	}
	if req.ThinPack {
		buf = pktline.AppendString(buf, "thin-pack\n")
	}
	if req.Progress == nil {
		buf = pktline.AppendString(buf, "no-progress\n")
	}
	if req.IncludeTag {
		buf = pktline.AppendString(buf, "include-tag\n")
	}
	buf = pktline.AppendString(buf, "ofs-delta\n")
	for _, commit := range req.Shallow {
		buf = pktline.AppendString(buf, "shallow "+commit.String()+"\n")
	}
	if req.Depth > 0 {
		buf = pktline.AppendString(buf, fmt.Sprintf("deepen %d\n", req.Depth))
		if req.DepthRelative {
			buf = pktline.AppendString(buf, "deepen-relative\n")
		}
	}
	if !req.Since.IsZero() {
		buf = pktline.AppendString(buf, fmt.Sprintf("deepen-since %d\n", req.Since.Unix()))
	}
	for _, rev := range req.ShallowExclude {
		buf = pktline.AppendString(buf, "deepen-not "+rev+"\n")
	}
	if req.Filter != "" {
		buf = pktline.AppendString(buf, "filter "+req.Filter+"\n")
	}
	buf = pktline.AppendFlush(buf)
	return buf
}

func readFetchOutputV2(r *pktline.Reader, newPackfileReader func(*pktline.Reader) io.ReadCloser) (*FetchResponse, error) {
	r.Next()
	section, err := r.Text()
	if err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	result := new(FetchResponse)

	if bytes.Equal(section, []byte("acknowledgments")) {
		var err error
		result.Acks, err = readAcksSectionV2(r)
		if err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		if r.Type() == pktline.Flush {
			// Response is allowed to end after acknowledgements.
			return result, nil
		}
		if r.Type() != pktline.Delim {
			return nil, fmt.Errorf("parse response: parse acknowledgements: expected flush or delim at end")
		}
		r.Next()
		section, err = r.Text()
		if err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
	}

	if bytes.Equal(section, []byte("shallow-info")) {
		var err error
		result.Shallow, err = readShallowInfoSectionV2(r)
		if err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		if r.Type() != pktline.Delim {
			return nil, fmt.Errorf("parse response: parse shallow info: expected delim at end")
		}
		r.Next()
		section, err = r.Text()
		if err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
	}

	if !bytes.Equal(section, []byte("packfile")) {
		return nil, fmt.Errorf("parse response: unexpected section %q", section)
	}
	result.Packfile = newPackfileReader(r)
	return result, nil
}

const (
	ackPrefix = "ACK "
	nak       = "NAK"
)

func readAcksSectionV2(r *pktline.Reader) (map[githash.SHA1]struct{}, error) {
	acks := make(map[githash.SHA1]struct{})
	for r.Next() && r.Type() == pktline.Data {
		line, err := r.Text()
		if err != nil {
			return nil, fmt.Errorf("parse acknowledgements: %w", err)
		}
		switch {
		case bytes.HasPrefix(line, []byte(ackPrefix)):
			var id githash.SHA1
			if err := id.UnmarshalText(line[len(ackPrefix):]); err != nil {
				return nil, fmt.Errorf("parse acknowledgements: %w", err)
			}
			acks[id] = struct{}{}
		case bytes.Equal(line, []byte(nak)):
		case bytes.Equal(line, []byte("ready")):
		default:
			return nil, fmt.Errorf("parse acknowledgements: unrecognized directive %q", line)
		}
	}
	if err := r.Err(); err != nil {
		return nil, fmt.Errorf("parse acknowledgements: %w", err)
	}
	return acks, nil
}

const (
	shallowPrefix   = "shallow "
	unshallowPrefix = "unshallow "
)

func readShallowInfoSectionV2(r *pktline.Reader) (map[githash.SHA1]bool, error) {
	result := make(map[githash.SHA1]bool)
	for r.Next() && r.Type() == pktline.Data {
		line, err := r.Text()
		if err != nil {
			return nil, fmt.Errorf("parse shallow info: %w", err)
		}
		var commitID githash.SHA1
		var isShallow bool
		switch {
		case bytes.HasPrefix(line, []byte(shallowPrefix)):
			isShallow = true
			if err := commitID.UnmarshalText(line[len(shallowPrefix):]); err != nil {
				return nil, fmt.Errorf("parse shallow info: %w", err)
			}
		case bytes.HasPrefix(line, []byte(unshallowPrefix)):
			isShallow = false
			if err := commitID.UnmarshalText(line[len(unshallowPrefix):]); err != nil {
				return nil, fmt.Errorf("parse shallow info: %w", err)
			}
		default:
			return nil, fmt.Errorf("parse shallow info: unrecognized directive %q", line)
		}
		result[commitID] = isShallow
	}
	if err := r.Err(); err != nil {
		return nil, fmt.Errorf("parse shallow info: %w", err)
	}
	return result, nil
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
		k, v, err := parseCapability(line)
		if err != nil {
			return nil, fmt.Errorf("parse capability advertisement: %w", err)
		}
		caps[k] = v
	}
	if err := r.Err(); err != nil {
		return nil, fmt.Errorf("parse capability advertisement: %w", err)
	}
	return caps, nil
}
