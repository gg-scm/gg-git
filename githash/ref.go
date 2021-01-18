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

package githash

import "strings"

// A Ref is a Git reference to a commit.
type Ref string

// Top-level refs.
const (
	// Head names the commit on which the changes in the working tree
	// are based.
	Head Ref = "HEAD"

	// FetchHead records the branch which was fetched from a remote
	// repository with the last git fetch invocation.
	FetchHead Ref = "FETCH_HEAD"
)

// BranchRef returns a ref for the given branch name.
func BranchRef(b string) Ref {
	return branchPrefix + Ref(b)
}

// TagRef returns a ref for the given tag name.
func TagRef(t string) Ref {
	return tagPrefix + Ref(t)
}

// IsValid reports whether r is a valid reference name.
// See https://git-scm.com/docs/git-check-ref-format for rules.
func (r Ref) IsValid() bool {
	return r != "" && r != "@" &&
		r[0] != '-' && r[0] != '.' && r[0] != '/' &&
		r[len(r)-1] != '.' && r[len(r)-1] != '/' &&
		strings.IndexFunc(string(r), func(c rune) bool {
			return c < 0x20 || c == 0x7f ||
				c == ' ' || c == '~' || c == '^' || c == ':' ||
				c == '?' || c == '*' || c == '[' ||
				c == '\\'
		}) < 0 &&
		!strings.Contains(string(r), "..") &&
		!strings.Contains(string(r), "@{") &&
		!strings.Contains(string(r), "//") &&
		!strings.Contains(string(r), "/.") &&
		!strings.Contains(string(r), ".lock/") &&
		!strings.HasSuffix(string(r), ".lock")
}

// String returns the ref as a string.
func (r Ref) String() string {
	return string(r)
}

// Ref prefixes.
const (
	branchPrefix = "refs/heads/"
	tagPrefix    = "refs/tags/"
)

// IsBranch reports whether r starts with "refs/heads/".
func (r Ref) IsBranch() bool {
	return r.IsValid() && strings.HasPrefix(string(r), branchPrefix)
}

// Branch returns the string after "refs/heads/" or an empty string
// if the ref does not start with "refs/heads/".
func (r Ref) Branch() string {
	if !r.IsBranch() {
		return ""
	}
	return string(r[len(branchPrefix):])
}

// IsTag reports whether r starts with "refs/tags/".
func (r Ref) IsTag() bool {
	return r.IsValid() && strings.HasPrefix(string(r), tagPrefix)
}

// Tag returns the string after "refs/tags/" or an empty string
// if the ref does not start with "refs/tags/".
func (r Ref) Tag() string {
	if !r.IsTag() {
		return ""
	}
	return string(r[len(tagPrefix):])
}
