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

package githash_test

import (
	"context"
	"strings"
	"testing"

	"gg-scm.io/pkg/git"
	. "gg-scm.io/pkg/git/githash"
)

func TestRef(t *testing.T) {
	tests := []struct {
		ref      Ref
		invalid  bool
		isBranch bool
		branch   string
		isTag    bool
		tag      string
	}{
		{
			ref:     "",
			invalid: true,
		},
		{
			ref:     "-",
			invalid: true,
		},
		{
			ref:     "@",
			invalid: true,
		},
		{ref: "main"},
		{ref: "HEAD"},
		{ref: "FETCH_HEAD"},
		{ref: "ORIG_HEAD"},
		{ref: "MERGE_HEAD"},
		{ref: "CHERRY_PICK_HEAD"},
		{ref: "FOO"},
		{ref: "refs/foo"},
		{
			ref:     "-refs/heads/main",
			invalid: true,
		},
		{
			ref:      "refs/heads/main-",
			isBranch: true,
			branch:   "main-",
		},
		{
			ref:     "refs/heads/",
			invalid: true,
		},
		{
			ref:     "/refs/heads/main",
			invalid: true,
		},
		{
			ref:      "refs/heads/main",
			isBranch: true,
			branch:   "main",
		},
		{
			ref:     "refs/heads//main",
			invalid: true,
		},
		{
			ref:     "refs/heads/.foo",
			invalid: true,
		},
		{
			ref:     "refs/heads/foo.",
			invalid: true,
		},
		{
			ref:      "refs/heads/foo.bar",
			isBranch: true,
			branch:   "foo.bar",
		},
		{
			ref:      "refs/heads/foo./bar",
			isBranch: true,
			branch:   "foo./bar",
		},
		{
			ref:     "refs/heads/foo..bar",
			invalid: true,
		},
		{
			ref:     "refs/heads/foo.lock",
			invalid: true,
		},
		{
			ref:      "refs/heads/foo.lock.bar",
			isBranch: true,
			branch:   "foo.lock.bar",
		},
		{
			ref:     "refs/heads/foo.lock/bar",
			invalid: true,
		},
		{
			ref:     "refs/heads/main:bar",
			invalid: true,
		},
		{
			ref:     "refs/heads/foo bar",
			invalid: true,
		},
		{
			ref:     "refs/heads/foo~bar",
			invalid: true,
		},
		{
			ref:     "refs/heads/foo^bar",
			invalid: true,
		},
		{
			ref:     "refs/heads/foo*bar",
			invalid: true,
		},
		{
			ref:     "refs/heads/foo?bar",
			invalid: true,
		},
		{
			ref:     "refs/heads/foo[bar",
			invalid: true,
		},
		{
			ref:      "refs/heads/@",
			isBranch: true,
			branch:   "@",
		},
		{
			ref:      "refs/heads/foo@bar",
			isBranch: true,
			branch:   "foo@bar",
		},
		{
			ref:      "refs/heads/foo{bar",
			isBranch: true,
			branch:   "foo{bar",
		},
		{
			ref:     "refs/heads/foo@{bar",
			invalid: true,
		},
		{
			ref:     "refs/heads/foo\\bar",
			invalid: true,
		},
		{
			ref:     "refs/heads/foo\x00bar",
			invalid: true,
		},
		{
			ref:     "refs/heads/foo\x1fbar",
			invalid: true,
		},
		{
			ref:     "refs/heads/foo\x7fbar",
			invalid: true,
		},
		{
			ref:      "refs/heads/foo\u0080bar",
			isBranch: true,
			branch:   "foo\u0080bar",
		},
		{
			ref:      "refs/heads/föö",
			isBranch: true,
			branch:   "föö",
		},
		{
			ref:   "refs/tags/v1.2.3",
			isTag: true,
			tag:   "v1.2.3",
		},
		{ref: "refs/for/main"},
		{ref: "does/not/start/with/refs"},
		{ref: "objects/foo"},
	}
	for _, test := range tests {
		if got := test.ref.String(); got != string(test.ref) {
			t.Errorf("Ref(%q).String() = %q; want %q", string(test.ref), got, string(test.ref))
		}
		if got := test.ref.IsValid(); got != !test.invalid {
			t.Errorf("Ref(%q).IsValid() = %t; want %t", string(test.ref), got, !test.invalid)
		}
		if got := test.ref.IsBranch(); got != test.isBranch {
			t.Errorf("Ref(%q).IsBranch() = %t; want %t", string(test.ref), got, test.isBranch)
		}
		if got := test.ref.Branch(); got != test.branch {
			t.Errorf("Ref(%q).Branch() = %q; want %q", string(test.ref), got, test.branch)
		}
		if got := test.ref.IsTag(); got != test.isTag {
			t.Errorf("Ref(%q).IsTag() = %t; want %t", string(test.ref), got, test.isTag)
		}
		if got := test.ref.Tag(); got != test.tag {
			t.Errorf("Ref(%q).Tag() = %q; want %q", string(test.ref), got, test.tag)
		}
	}

	t.Run("VerifyValidWithGit", func(t *testing.T) {
		ctx := context.Background()
		g, err := git.New(git.Options{})
		if err != nil {
			t.Skip("Could not find Git:", err)
		}
		for _, test := range tests {
			if strings.Contains(string(test.ref), "\x00") {
				if !test.invalid {
					t.Errorf("ref %q with NUL byte marked valid in test table", test.ref)
				}
				continue
			}
			err := g.Run(ctx, "check-ref-format", "--allow-onelevel", string(test.ref))
			if err == nil && test.invalid {
				t.Errorf("git check-ref-format %q reports valid, but test table expects invalid", test.ref)
			} else if err != nil && !test.invalid {
				t.Errorf("git check-ref-format %q: %v (test table expects valid)", test.ref, err)
			}
		}
	})
}
