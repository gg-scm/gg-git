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

// Reference:
// https://git-scm.com/docs/pack-protocol
// https://git-scm.com/docs/http-protocol (because we're using stateless)

const v1ExtraParams = "version=1"

// Capability names. See https://git-scm.com/docs/protocol-capabilities
const (
	deepenRelativeCap = "deepen-relative"
	deepenSinceCap    = "deepen-since"
	deepenNotCap      = "deepen-not"
	filterCap         = "filter"
	includeTagCap     = "include-tag"
	multiAckCap       = "multi_ack"
	noProgressCap     = "no-progress"
	ofsDeltaCap       = "ofs-delta"
	shallowCap        = "shallow"
	sideBand64KCap    = "side-band-64k"
	sideBandCap       = "side-band"
	symrefCap         = "symref"
	thinPackCap       = "thin-pack"
)

type pullV1 struct {
	caps       capabilityList
	impl       impl
	refsReader *pktline.Reader
	refsCloser io.Closer

	refs      []*Ref
	refsError error
}

func newPullV1(impl impl, refsReader *pktline.Reader, refsCloser io.Closer) *pullV1 {
	p := &pullV1{impl: impl}
	var ref0 *Ref
	ref0, p.caps, p.refsError = readFirstRefV1(refsReader)
	if ref0 == nil {
		// Either an error or only capabilities were received.
		// No need to hang onto refsReader.
		refsCloser.Close()
		return p
	}
	p.refs = []*Ref{ref0}
	p.refsReader = refsReader
	p.refsCloser = refsCloser
	return p
}

func (p *pullV1) Close() error {
	if p.refsCloser != nil {
		return p.refsCloser.Close()
	}
	return nil
}

func (p *pullV1) listRefs(ctx context.Context, refPrefixes []string) ([]*Ref, error) {
	if p.refsReader != nil {
		p.refs, p.refsError = readOtherRefsV1(p.refs, p.caps.symrefs(), p.refsReader)
		p.refsCloser.Close()
		p.refsReader = nil
		p.refsCloser = nil
	}
	if len(refPrefixes) == 0 {
		return append([]*Ref(nil), p.refs...), p.refsError
	}
	// Filter by given prefixes.
	refs := make([]*Ref, 0, len(p.refs))
	for _, r := range p.refs {
		for _, prefix := range refPrefixes {
			if strings.HasPrefix(string(r.Name), prefix) {
				refs = append(refs, r)
			}
		}
	}
	return refs, p.refsError
}

// readFirstRefV1 reads the first ref in the version 1 refs advertisement
// response, skipping the "version 1" line if necessary. The caller is expected
// to have advanced r to the first line before calling readFirstRefV1.
func readFirstRefV1(r *pktline.Reader) (*Ref, capabilityList, error) {
	line, err := r.Text()
	if err != nil {
		return nil, nil, fmt.Errorf("read refs: first ref: %w", err)
	}
	if bytes.Equal(line, []byte("version 1")) {
		// Skip optional initial "version 1" packet.
		r.Next()
		line, err = r.Text()
		if err != nil {
			return nil, nil, fmt.Errorf("read refs: first ref: %w", err)
		}
	}
	ref0, caps, err := parseFirstRefV1(line)
	if err != nil {
		return nil, nil, fmt.Errorf("read refs: %w", err)
	}
	if ref0 == nil {
		// Expect flush next.
		// TODO(someday): Or shallow?
		if !r.Next() {
			return nil, nil, fmt.Errorf("read refs: %w", r.Err())
		}
		if r.Type() != pktline.Flush {
			return nil, nil, fmt.Errorf("read refs: expected flush after no-refs")
		}
		return nil, caps, nil
	}
	return ref0, caps, nil
}

func parseFirstRefV1(line []byte) (*Ref, capabilityList, error) {
	refEnd := bytes.IndexByte(line, 0)
	if refEnd == -1 {
		return nil, nil, fmt.Errorf("first ref: missing nul")
	}
	idEnd := bytes.IndexByte(line[:refEnd], ' ')
	if idEnd == -1 {
		return nil, nil, fmt.Errorf("first ref: missing space")
	}
	id, err := githash.ParseSHA1(string(line[:idEnd]))
	if err != nil {
		return nil, nil, fmt.Errorf("first ref: %w", err)
	}
	refName := githash.Ref(line[idEnd+1 : refEnd])
	caps := make(capabilityList)
	for _, c := range bytes.Fields(line[refEnd+1:]) {
		k, v, err := parseCapability(c)
		if err != nil {
			return nil, nil, fmt.Errorf("first ref: %w", err)
		}
		if k == symrefCap {
			caps.addSymref(v)
		} else {
			caps[k] = v
		}
	}
	if refName == "capabilities^{}" {
		if id != (githash.SHA1{}) {
			return nil, nil, fmt.Errorf("first ref: non-zero ID passed with no-refs response")
		}
		return nil, caps, nil
	}
	if !refName.IsValid() {
		return nil, nil, fmt.Errorf("first ref %q: invalid name", refName)
	}
	return &Ref{
		ObjectID:     id,
		Name:         refName,
		SymrefTarget: caps.symrefs()[refName],
	}, caps, nil
}

// readOtherRefsV1 parses the second and subsequent refs in the version 1 refs
// advertisement response. The caller is expected to have advanced r past the
// first ref before calling readOtherRefsV1.
func readOtherRefsV1(refs []*Ref, symrefs map[githash.Ref]githash.Ref, r *pktline.Reader) ([]*Ref, error) {
	for r.Next() && r.Type() != pktline.Flush {
		line, err := r.Text()
		if err != nil {
			return nil, fmt.Errorf("read refs: %w", err)
		}
		ref, err := parseOtherRefV1(line)
		if err != nil {
			return nil, fmt.Errorf("read refs: %w", err)
		}
		ref.SymrefTarget = symrefs[ref.Name]
		refs = append(refs, ref)
	}
	if err := r.Err(); err != nil {
		return refs, fmt.Errorf("read refs: %w", err)
	}
	return refs, nil
}

func parseOtherRefV1(line []byte) (*Ref, error) {
	idEnd := bytes.IndexByte(line, ' ')
	if idEnd == -1 {
		return nil, fmt.Errorf("ref: missing space")
	}
	refName := githash.Ref(line[idEnd+1:])
	if !refName.IsValid() {
		return nil, fmt.Errorf("ref %q: invalid name", refName)
	}
	id, err := githash.ParseSHA1(string(line[:idEnd]))
	if err != nil {
		return nil, fmt.Errorf("ref %s: %w", refName, err)
	}
	return &Ref{
		ObjectID: id,
		Name:     refName,
	}, nil
}

func (p *pullV1) capabilities() PullCapabilities {
	caps := PullCapabilities(0)
	if p.caps.supports(shallowCap) {
		caps |= PullCapShallow
	}
	if p.caps.supports(deepenRelativeCap) {
		caps |= PullCapDepthRelative
	}
	if p.caps.supports(deepenSinceCap) {
		caps |= PullCapSince
	}
	if p.caps.supports(deepenNotCap) {
		caps |= PullCapShallowExclude
	}
	if p.caps.supports(filterCap) {
		caps |= PullCapFilter
	}
	if p.caps.supports(includeTagCap) {
		caps |= PullCapIncludeTag
	}
	if p.caps.supports(thinPackCap) {
		caps |= PullCapThinPack
	}
	return caps
}

func (p *pullV1) negotiate(ctx context.Context, errPrefix string, req *PullRequest) (*PullResponse, error) {
	useCaps, err := capabilitiesToSendV1(req, p.caps)
	if err != nil {
		return nil, err
	}
	commandBuf := formatUploadRequestV1(req, useCaps)
	resp, err := p.impl.uploadPack(ctx, v1ExtraParams, bytes.NewReader(commandBuf))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			resp.Close()
		}
	}()

	respReader := pktline.NewReader(resp)
	result := &PullResponse{
		Acks: make(map[githash.SHA1]struct{}),
	}
	if req.Depth > 0 || !req.Since.IsZero() || len(req.ShallowExclude) > 0 {
		// "If the client sent a positive depth request, the server will determine
		// which commits will and will not be shallow and send this information
		// to the client."
		var err error
		result.Shallow, err = readShallowUpdateV1(respReader)
		if err != nil {
			return nil, err
		}
	}
	var foundCommonBase bool
	result.Acks, foundCommonBase, err = readServerResponseV1(respReader)
	if err != nil {
		return nil, err
	}
	if foundCommonBase || !req.HaveMore {
		result.Packfile = &packfileReader{
			errPrefix:  errPrefix,
			packReader: respReader,
			packCloser: resp,
			progress:   req.Progress,
		}
	}
	return result, nil
}

func capabilitiesToSendV1(req *PullRequest, remoteCaps capabilityList) (capabilityList, error) {
	useCaps := capabilityList{
		multiAckCap: "",
		ofsDeltaCap: "",
	}
	if req.Progress == nil {
		useCaps[noProgressCap] = ""
	}
	if req.needsShallow() {
		useCaps[shallowCap] = ""
	}
	if req.needsDepthRelative() {
		useCaps[deepenRelativeCap] = ""
	}
	if !req.Since.IsZero() {
		useCaps[deepenSinceCap] = ""
	}
	if len(req.ShallowExclude) > 0 {
		useCaps[deepenNotCap] = ""
	}
	if req.Filter != "" {
		useCaps[filterCap] = ""
	}
	if req.IncludeTag {
		useCaps[includeTagCap] = ""
	}
	if req.ThinPack {
		useCaps[thinPackCap] = ""
	}
	useCaps.intersect(remoteCaps)
	// From https://git-scm.com/docs/protocol-capabilities, "[t]he client MUST
	// send only maximum [sic] of one of 'side-band' and [sic] 'side-band-64k'."
	switch {
	case remoteCaps.supports(sideBand64KCap):
		useCaps[sideBand64KCap] = ""
	case remoteCaps.supports(sideBandCap):
		useCaps[sideBandCap] = ""
	default:
		// TODO(someday): Support reading without demuxing.
		return nil, fmt.Errorf("remote does not support %s", sideBandCap)
	}
	return useCaps, nil
}

func formatUploadRequestV1(req *PullRequest, useCaps capabilityList) []byte {
	var buf []byte
	buf = pktline.AppendString(buf, fmt.Sprintf("want %v %v\n", req.Want[0], useCaps))
	for _, want := range req.Want[1:] {
		buf = pktline.AppendString(buf, "want "+want.String()+"\n")
	}
	for _, shallow := range req.Shallow {
		buf = pktline.AppendString(buf, "shallow "+shallow.String()+"\n")
	}
	if req.Depth > 0 {
		buf = pktline.AppendString(buf, fmt.Sprintf("deepen %d\n", req.Depth))
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

	for _, have := range req.Have {
		buf = pktline.AppendString(buf, "have "+have.String()+"\n")
	}
	if !req.HaveMore {
		buf = pktline.AppendString(buf, "done")
	} else {
		buf = pktline.AppendFlush(buf)
	}
	return buf
}

func readShallowUpdateV1(r *pktline.Reader) (map[githash.SHA1]bool, error) {
	result := make(map[githash.SHA1]bool)
	for r.Next() && r.Type() != pktline.Flush {
		line, err := r.Text()
		if err != nil {
			return nil, fmt.Errorf("parse shallow update: %w", err)
		}
		var commitID githash.SHA1
		var isShallow bool
		switch {
		case bytes.HasPrefix(line, []byte(shallowPrefix)):
			isShallow = true
			if err := commitID.UnmarshalText(line[len(shallowPrefix):]); err != nil {
				return nil, fmt.Errorf("parse shallow update: %w", err)
			}
		case bytes.HasPrefix(line, []byte(unshallowPrefix)):
			isShallow = false
			if err := commitID.UnmarshalText(line[len(unshallowPrefix):]); err != nil {
				return nil, fmt.Errorf("parse shallow update: %w", err)
			}
		default:
			return nil, fmt.Errorf("parse shallow update: unrecognized directive %q", line)
		}
		result[commitID] = isShallow
	}
	if err := r.Err(); err != nil {
		return nil, fmt.Errorf("parse shallow update: %w", err)
	}
	return result, nil
}

func readServerResponseV1(r *pktline.Reader) (acks map[githash.SHA1]struct{}, foundCommonBase bool, err error) {
	acks = make(map[githash.SHA1]struct{})
	for r.Next() {
		line, err := r.Text()
		if err != nil {
			return nil, false, fmt.Errorf("parse response: %w", err)
		}
		switch {
		case bytes.HasPrefix(line, []byte(ackPrefix)):
			line = line[len(ackPrefix):]
			idEnd := bytes.IndexByte(line, ' ')
			statusStart := idEnd + 1
			if idEnd == -1 {
				idEnd = len(line)
				statusStart = idEnd
			}
			var id githash.SHA1
			if err := id.UnmarshalText(line[:idEnd]); err != nil {
				return nil, false, fmt.Errorf("parse response: acknowledgements: %w", err)
			}
			acks[id] = struct{}{}
			switch status := line[statusStart:]; {
			case len(status) == 0:
				return acks, true, nil
			case bytes.Equal(status, []byte("continue")):
				// Only valid status for multi_ack
			default:
				return nil, false, fmt.Errorf("parse response: acknowledgements: unknown status %q", status)
			}
		case bytes.Equal(line, []byte(nak)):
			return acks, false, nil
		default:
			return nil, false, fmt.Errorf("parse response: acknowledgements: unrecognized directive %q", line)
		}
	}
	return nil, false, fmt.Errorf("parse response: %w", err)
}
