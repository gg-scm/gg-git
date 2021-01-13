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
	"sort"
	"strings"
	"time"

	"gg-scm.io/pkg/git/githash"
	"gg-scm.io/pkg/git/internal/pktline"
)

// PullStream represents a git-upload-pack session.
type PullStream struct {
	ctx    context.Context
	urlstr string
	impl   puller
}

type puller interface {
	negotiate(ctx context.Context, errPrefix string, req *PullRequest) (*PullResponse, error)
	listRefs(ctx context.Context, refPrefixes []string) ([]*Ref, error)
	capabilities() PullCapabilities
	Close() error
}

// StartPull starts a git-upload-pack session on the remote.
// The Context is used for the entire pull stream. The caller is responsible
// for calling Close on the returned PullStream.
func (r *Remote) StartPull(ctx context.Context) (_ *PullStream, err error) {
	resp, err := r.impl.advertiseRefs(ctx, r.pullExtraParams)
	if err != nil {
		return nil, fmt.Errorf("pull %s: %w", r.urlstr, err)
	}
	// Not deferring resp.Close because V1 needs to retain resp.
	respReader := pktline.NewReader(resp)
	respReader.Next()
	line, err := respReader.Text()
	if err != nil {
		resp.Close()
		return nil, fmt.Errorf("pull %s: %w", r.urlstr, err)
	}
	p := &PullStream{
		ctx:    ctx,
		urlstr: r.urlstr,
	}
	if bytes.Equal(line, []byte(version2Line)) {
		caps, err := parseCapabilityAdvertisementV2(respReader)
		resp.Close()
		if err != nil {
			return nil, fmt.Errorf("pull %s: %w", r.urlstr, err)
		}
		p.impl = &pullV2{
			caps: caps,
			impl: r.impl,
		}
	} else {
		p.impl = newPullV1(r.impl, respReader, resp)
	}
	return p, nil
}

// Ref describes a single reference to a Git object.
type Ref struct {
	ObjectID     githash.SHA1
	Name         githash.Ref
	SymrefTarget githash.Ref
}

// ListRefs lists the remote's references. If refPrefixes is given, then only
// refs that start with one of the given strings are returned.
//
// If you need to call both ListRefs and Negotiate on a stream, you should call
// ListRefs first. Older Git servers send their refs upfront
func (p *PullStream) ListRefs(refPrefixes ...string) ([]*Ref, error) {
	refs, err := p.impl.listRefs(p.ctx, refPrefixes)
	if err != nil {
		return nil, fmt.Errorf("list refs for %s: %w", p.urlstr, err)
	}
	return refs, nil
}

// Capabilities returns the set of pull request fields the remote supports.
func (p *PullStream) Capabilities() PullCapabilities {
	return p.impl.capabilities()
}

// A PullRequest informs the remote which objects to include in the packfile.
type PullRequest struct {
	// Want is the set of commits to send. At least one must be specified,
	// or SendRequest will return an error.
	Want []githash.SHA1
	// Have is a set of commits that the remote can exclude from the packfile.
	// It may be empty for a full clone. The remote will also avoid sending any
	// trees and blobs used in the Have commits and any of their ancestors, even
	// if they're used in returned commits.
	Have []githash.SHA1
	// If HaveMore is true, then the response will not return a packfile if the
	// remote hasn't found a suitable base.
	HaveMore bool

	// Progress will receive progress messages from the remote while the caller
	// reads the packfile. It may be nil.
	Progress io.Writer

	// Shallow is the set of object IDs that the client does not have the parent
	// commits of. This is only supported by the remote if it has PullCapShallow.
	Shallow []githash.SHA1

	// If Depth is greater than zero, it limits the depth of the commits pulled.
	// It is mutually exclusive with Since. This is only supported by the remote
	// if it has PullCapShallow.
	Depth int
	// If DepthRelative is true, Depth is interpreted as relative to the client's
	// shallow boundary. Otherwise, it is interpreted as relative to the commits
	// in Want. This is only supported by the remote if it has PullCapDepthRelative.
	DepthRelative bool
	// Since requests that the shallow clone/pull should be cut at a specific
	// time. It is mutually exclusive with Depth.  This is only supported by the
	// remote if it has PullCapSince.
	Since time.Time
	// ShallowExclude is a set of revisions that the remote will exclude from
	// the packfile. Unlike Have, the remote will send any needed trees and
	// blobs even if they are shared with the revisions in ShallowExclude.
	// It is mutually exclusive with Depth, but not Since. This is only supported
	// by the remote if it has PullCapShallowExclude.
	ShallowExclude []string

	// Filter filters objects in the packfile based on a filter-spec as defined
	// in git-rev-list(1). This is only supported by the remote if it has PullCapFilter.
	Filter string

	// IncludeTag indicates whether annotated tags should be sent if the objects
	// they point to are being sent. This is only supported by the remote if it
	// has PullCapIncludeTag.
	IncludeTag bool

	// ThinPack requests that a thin pack be sent, which is a pack with deltas
	// which reference base objects not contained within the pack (but are known
	// to exist at the receiving end). This can reduce the network traffic
	// significantly, but it requires the receiving end to know how to "thicken"
	// these packs by adding the missing bases to the pack. This is only supported
	// by the remote if it has PullCapThinPack.
	ThinPack bool
}

func (req *PullRequest) needsShallow() bool {
	return len(req.Shallow) > 0 || req.Depth > 0
}

func (req *PullRequest) needsDepthRelative() bool {
	return req.DepthRelative && req.Depth > 0
}

// A PullResponse holds the remote response to a round of negotiation.
type PullResponse struct {
	// Packfile is a packfile stream. It may be nil if the request set HaveMore
	// and the remote didn't find a suitable base.
	//
	// Any progress messages sent by the remote will be written to the Progress
	// writer specified in the request while reading from Packfile.
	Packfile io.ReadCloser
	// Acks indicates which of the Have objects from the request that the remote
	// shares. It may not be populated if Packfile is not nil.
	Acks map[githash.SHA1]struct{}
	// Shallow indicates each commit sent whose parents will not be in the
	// packfile. If a commit hash is in the Shallow map but its value is false,
	// it means that the request indicated the commit was shallow, but its parents
	// are present in the packfile.
	Shallow map[githash.SHA1]bool
}

// Negotiate requests a packfile from the remote. It must be called before
// calling the Read method.
func (p *PullStream) Negotiate(req *PullRequest) (*PullResponse, error) {
	// Validate request is self-consistent.
	errPrefix := "pull " + p.urlstr
	if len(req.Want) == 0 {
		return nil, fmt.Errorf("%s: no objects requested", errPrefix)
	}
	if req.Depth > 0 && !req.Since.IsZero() {
		return nil, fmt.Errorf("%s: Depth used with Since", errPrefix)
	}
	if req.Depth > 0 && len(req.ShallowExclude) > 0 {
		return nil, fmt.Errorf("%s: Depth used with ShallowExclude", errPrefix)
	}

	// Validate that request uses capabilities that the remote supports.
	caps := p.impl.capabilities()
	if (req.needsShallow()) && !caps.Has(PullCapShallow) {
		return nil, fmt.Errorf("%s: remote does not support shallow clones", errPrefix)
	}
	if req.needsDepthRelative() && !caps.Has(PullCapDepthRelative) {
		return nil, fmt.Errorf("%s: remote does not support relative depths", errPrefix)
	}
	if req.Since.IsZero() && !caps.Has(PullCapSince) {
		return nil, fmt.Errorf("%s: remote does not support shallow-since", errPrefix)
	}
	if len(req.ShallowExclude) > 0 && !caps.Has(PullCapShallowExclude) {
		return nil, fmt.Errorf("%s: remote does not support shallow revision exclusions", errPrefix)
	}
	if req.Filter != "" && !caps.Has(PullCapFilter) {
		return nil, fmt.Errorf("%s: remote does not support object filters", errPrefix)
	}
	if req.IncludeTag && !caps.Has(PullCapIncludeTag) {
		return nil, fmt.Errorf("%s: remote does not support include-tag", errPrefix)
	}
	if req.ThinPack && !caps.Has(PullCapThinPack) {
		return nil, fmt.Errorf("%s: remote does not support thin packs", errPrefix)
	}

	// Call negotiate.
	resp, err := p.impl.negotiate(p.ctx, errPrefix, req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	if resp.Packfile == nil && !req.HaveMore {
		return nil, fmt.Errorf("%s: remote did not send a packfile", errPrefix)
	}
	return resp, nil
}

// Close releases any resources used by the stream.
func (p *PullStream) Close() error {
	return p.impl.Close()
}

// packfileReader reads multiplexed packfile data.
type packfileReader struct {
	errPrefix  string
	packReader *pktline.Reader
	packCloser io.Closer

	curr      []byte // current packfile packet
	packError error
	progress  io.Writer
}

func (pr *packfileReader) Read(p []byte) (int, error) {
	if len(pr.curr) > 0 {
		n := copy(p, pr.curr)
		pr.curr = pr.curr[n:]
		return n, nil
	}
	if pr.packError != nil {
		return 0, pr.packError
	}
	n, err := pr.read(p)
	if err != nil {
		pr.packError = err
	}
	return n, err
}

func (pr *packfileReader) read(p []byte) (int, error) {
	for pr.packReader.Next() && pr.packReader.Type() == pktline.Data {
		pkt, err := pr.packReader.Bytes()
		if err != nil {
			return 0, err
		}
		if len(pkt) == 0 {
			return 0, fmt.Errorf("%s: empty packet", pr.errPrefix)
		}
		pktType, data := pkt[0], pkt[1:]
		switch pktType {
		case 1:
			// Pack data
			n := copy(p, data)
			pr.curr = data[n:]
			return n, nil
		case 2:
			// Progress message
			if pr.progress != nil {
				pr.progress.Write(data)
			}
		case 3:
			// Fatal error message
			return 0, fmt.Errorf("%s: server error: %s", pr.errPrefix, trimLF(data))
		default:
			return 0, fmt.Errorf("%s: encountered bad stream code (%02x)", pr.errPrefix, pktType)
		}
	}
	if err := pr.packReader.Err(); err != nil {
		return 0, fmt.Errorf("%s: %w", pr.errPrefix, err)
	}
	return 0, io.EOF
}

func trimLF(line []byte) []byte {
	if len(line) == 0 || line[len(line)-1] != '\n' {
		return line
	}
	return line[:len(line)-1]
}

func (pr *packfileReader) Close() error {
	return pr.packCloser.Close()
}

// PullCapabilities is a bitset of capabilities that a remote supports for pulling.
type PullCapabilities uint64

// Pull capabilities.
// See https://git-scm.com/docs/protocol-capabilities for descriptions.
const (
	PullCapShallow        PullCapabilities = 1 << iota // shallow
	PullCapDepthRelative                               // deepen-relative
	PullCapSince                                       // deepen-since
	PullCapShallowExclude                              // deepen-not
	PullCapFilter                                      // filter
	PullCapIncludeTag                                  // include-tag
	PullCapThinPack                                    // thin-pack

	maxPullCapBit
)

// Has reports whether caps includes all of the capabilities in mask.
func (caps PullCapabilities) Has(mask PullCapabilities) bool {
	return caps&mask == mask
}

// String returns a |-separated list of the capability constant names present
// in caps.
func (caps PullCapabilities) String() string {
	sb := new(strings.Builder)
	for bit := PullCapabilities(1); bit < maxPullCapBit; bit <<= 1 {
		if !caps.Has(bit) {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("|")
		}
		switch bit {
		case PullCapShallow:
			sb.WriteString("PullCapShallow")
		case PullCapDepthRelative:
			sb.WriteString("PullCapDepthRelative")
		case PullCapSince:
			sb.WriteString("PullCapSince")
		case PullCapShallowExclude:
			sb.WriteString("PullCapShallowExclude")
		case PullCapFilter:
			sb.WriteString("PullCapFilter")
		case PullCapIncludeTag:
			sb.WriteString("PullCapIncludeTag")
		case PullCapThinPack:
			sb.WriteString("PullCapThinPack")
		}
		caps &^= bit
	}
	if caps != 0 {
		if sb.Len() > 0 {
			sb.WriteString("|")
		}
		fmt.Fprintf(sb, "%#x", uint64(caps))
	}
	return sb.String()
}

// capabilityList is a set of capability tokens.
// See https://git-scm.com/docs/protocol-capabilities
type capabilityList map[string]string

func (caps capabilityList) supports(key string) bool {
	_, ok := caps[key]
	return ok
}

// symrefs parses the symrefCap value. It's the only repeated capability
// permitted, so it's stored as a space-separated string.
func (caps capabilityList) symrefs() map[githash.Ref]githash.Ref {
	words := strings.Fields(caps[symrefCap])
	if len(words) == 0 {
		return nil
	}
	m := make(map[githash.Ref]githash.Ref, len(words))
	for _, w := range words {
		i := strings.IndexByte(w, ':')
		if i == -1 {
			continue
		}
		sym := githash.Ref(w[:i])
		target := githash.Ref(w[i+1:])
		if !sym.IsValid() || !target.IsValid() {
			continue
		}
		m[sym] = target
	}
	return m
}

func (caps capabilityList) addSymref(elem string) {
	v := caps[symrefCap]
	if v != "" {
		v += " "
	}
	v += elem
	caps[symrefCap] = v
}

func (caps capabilityList) intersect(toIntersect capabilityList) {
	for c := range caps {
		if !toIntersect.supports(c) {
			delete(caps, c)
		}
	}
}

func parseCapability(word []byte) (string, string, error) {
	// TODO(soon): Verify that key and value have permitted characters.
	k, v := word, []byte(nil)
	if i := bytes.IndexByte(word, '='); i != -1 {
		k, v = word[:i], word[i+1:]
	}
	return string(k), string(v), nil
}

func (caps capabilityList) MarshalText() ([]byte, error) {
	if len(caps) == 0 {
		return nil, nil
	}
	// TODO(someday): Ensure keys and values use valid characters.
	keys := make([]string, len(caps))
	for k := range caps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf []byte
	for _, k := range keys {
		if k == symrefCap {
			// symrefCap is stored as space-separated values, but should be repeated.
			for _, v := range strings.Fields(caps[k]) {
				if len(buf) > 0 {
					buf = append(buf, ' ')
				}
				buf = append(buf, k...)
				buf = append(buf, '=')
				buf = append(buf, v...)
			}
			continue
		}
		if len(buf) > 0 {
			buf = append(buf, ' ')
		}
		buf = append(buf, k...)
		if v := caps[k]; v != "" {
			buf = append(buf, '=')
			buf = append(buf, v...)
		}
	}
	return buf, nil
}

func (caps capabilityList) String() string {
	text, _ := caps.MarshalText()
	return string(text)
}
