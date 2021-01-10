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

	"gg-scm.io/pkg/git/githash"
	"gg-scm.io/pkg/git/internal/pktline"
)

// FetchStream represents a git-upload-pack session.
type FetchStream struct {
	ctx    context.Context
	urlstr string
	impl   fetcher
}

type fetcher interface {
	negotiate(ctx context.Context, errPrefix string, req *FetchRequest) (*FetchResponse, error)
	listRefs(ctx context.Context, refPrefixes []string) ([]*Ref, error)
	Close() error
}

// StartFetch starts a git-upload-pack session on the remote.
// The Context is used for the entire fetch stream. The caller is responsible
// for calling Close on the returned FetchStream.
func (r *Remote) StartFetch(ctx context.Context) (_ *FetchStream, err error) {
	resp, err := r.impl.advertiseRefs(ctx, r.fetchExtraParams)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", r.urlstr, err)
	}
	// Not deferring resp.Close because V1 needs to retain resp.
	respReader := pktline.NewReader(resp)
	respReader.Next()
	line, err := respReader.Text()
	if err != nil {
		resp.Close()
		return nil, fmt.Errorf("fetch %s: %w", r.urlstr, err)
	}
	f := &FetchStream{
		ctx:    ctx,
		urlstr: r.urlstr,
	}
	if bytes.Equal(line, []byte(version2Line)) {
		caps, err := parseCapabilityAdvertisementV2(respReader)
		resp.Close()
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", r.urlstr, err)
		}
		f.impl = &fetchV2{
			caps: caps,
			impl: r.impl,
		}
	} else {
		f.impl = newFetchV1(r.impl, respReader, resp)
	}
	return f, nil
}

// Ref describes a single reference to a Git object.
type Ref struct {
	ID           githash.SHA1
	Name         githash.Ref
	SymrefTarget githash.Ref
}

// ListRefs lists the remote's references. If refPrefixes is given, then only
// refs that start with one of the given strings are returned.
//
// If you need to call both ListRefs and Negotiate on a stream, you should call
// ListRefs first. Older Git servers send their refs upfront
func (f *FetchStream) ListRefs(refPrefixes ...string) ([]*Ref, error) {
	refs, err := f.impl.listRefs(f.ctx, refPrefixes)
	if err != nil {
		return nil, fmt.Errorf("list refs for %s: %w", f.urlstr, err)
	}
	return refs, nil
}

// A FetchRequest informs the remote which objects to include in the packfile.
type FetchRequest struct {
	// Want is the set of object IDs to send. At least one must be specified,
	// or SendRequest will return an error.
	Want []githash.SHA1
	// Have is the set of object IDs that the server can exclude from the
	// packfile. It may be empty.
	Have []githash.SHA1
	// If HaveMore is true, then the response will not return a Packfile if the
	// remote hasn't found a suitable base.
	HaveMore bool

	// IncludeTag indicates whether annotated tags should be sent if the objects
	// they point to are being sent.
	IncludeTag bool
	// Progress will receive progress messages from the remote while the caller
	// reads the packfile. It may be nil.
	Progress io.Writer
}

// A FetchResponse holds the remote response to a round of negotiation.
type FetchResponse struct {
	// Packfile is a packfile stream. It may be nil if the request set HaveMore
	// and the remote didn't find a suitable base.
	//
	// Any progress messages sent by the remote will be written to the Progress
	// writer specified in the request while reading from Packfile.
	Packfile io.ReadCloser
	// Acks indicates which of the Have objects from the request that the remote
	// shares. It may not be populated if Packfile is not nil.
	Acks map[githash.SHA1]bool
}

// Negotiate requests a packfile from the remote. It must be called before
// calling the Read method.
func (f *FetchStream) Negotiate(req *FetchRequest) (*FetchResponse, error) {
	errPrefix := "fetch " + f.urlstr
	if len(req.Want) == 0 {
		return nil, fmt.Errorf("%s: no objects requested", errPrefix)
	}
	resp, err := f.impl.negotiate(f.ctx, errPrefix, req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	if resp.Packfile == nil && !req.HaveMore {
		return nil, fmt.Errorf("%s: remote did not send a packfile", errPrefix)
	}
	return resp, nil
}

// Close releases any resources used by the stream.
func (f *FetchStream) Close() error {
	return f.impl.Close()
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

func (f *packfileReader) Read(p []byte) (int, error) {
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

func (f *packfileReader) read(p []byte) (int, error) {
	for f.packReader.Next() && f.packReader.Type() == pktline.Data {
		pkt, err := f.packReader.Bytes()
		if err != nil {
			return 0, err
		}
		if len(pkt) == 0 {
			return 0, fmt.Errorf("%s: empty packet", f.errPrefix)
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
			return 0, fmt.Errorf("%s: server error: %s", f.errPrefix, trimLF(data))
		default:
			return 0, fmt.Errorf("%s: encountered bad stream code (%02x)", f.errPrefix, pktType)
		}
	}
	if err := f.packReader.Err(); err != nil {
		return 0, fmt.Errorf("%s: %w", f.errPrefix, err)
	}
	return 0, io.EOF
}

func trimLF(line []byte) []byte {
	if len(line) == 0 || line[len(line)-1] != '\n' {
		return line
	}
	return line[:len(line)-1]
}

func (f *packfileReader) Close() error {
	return f.packCloser.Close()
}

// Capability names. See https://git-scm.com/docs/protocol-capabilities
const (
	multiAckCap    = "multi_ack"
	noProgressCap  = "no-progress"
	ofsDeltaCap    = "ofs-delta"
	sideBand64KCap = "side-band-64k"
	sideBandCap    = "side-band"
	symrefCap      = "symref"
)

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
