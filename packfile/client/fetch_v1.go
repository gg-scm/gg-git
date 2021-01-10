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
	"fmt"
	"strings"

	"gg-scm.io/pkg/git/githash"
	"gg-scm.io/pkg/git/internal/pktline"
)

func readRefAdvertisementV1(r *pktline.Reader) ([]*Ref, error) {
	// First line is a ref but also includes capabilities.
	var refs []*Ref
	r.Next()
	line, err := r.Text()
	if err != nil {
		return nil, fmt.Errorf("read refs: first ref: %w", err)
	}
	if bytes.Equal(line, []byte("version 1")) {
		// Skip optional initial "version 1" packet.
		r.Next()
		line, err = r.Text()
		if err != nil {
			return nil, fmt.Errorf("read refs: first ref: %w", err)
		}
	}
	ref0, _, err := parseFirstRefV1(line)
	if err != nil {
		return nil, fmt.Errorf("read refs: %w", err)
	}
	if ref0 == nil {
		// Expect flush next.
		// TODO(someday): Or shallow?
		if !r.Next() {
			return nil, fmt.Errorf("read refs: %w", r.Err())
		}
		if r.Type() != pktline.Flush {
			return nil, fmt.Errorf("read refs: expected flush after no-refs")
		}
		return nil, nil
	}
	refs = append(refs, ref0)

	// Subsequent lines are just refs.
	for r.Next() && r.Type() != pktline.Flush {
		line, err := r.Text()
		if err != nil {
			return nil, fmt.Errorf("read refs: %w", err)
		}
		ref, err := parseOtherRefV1(line)
		if err != nil {
			return nil, fmt.Errorf("read refs: %w", err)
		}
		refs = append(refs, ref)
	}
	if err := r.Err(); err != nil {
		return nil, fmt.Errorf("read refs: %w", err)
	}
	return refs, nil
}

func parseFirstRefV1(line []byte) (*Ref, []string, error) {
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
	caps := strings.Fields(string(line[refEnd+1:]))
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
		ID:   id,
		Name: refName,
	}, caps, nil
}

func parseOtherRefV1(line []byte) (*Ref, error) {
	line = trimLF(line)
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
		ID:   id,
		Name: refName,
	}, nil
}
