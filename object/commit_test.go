// Copyright 2020 The gg Authors
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

package object

import (
	"bytes"
	"encoding"
	"testing"
	"time"

	"gg-scm.io/pkg/git/githash"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var (
	_ encoding.BinaryMarshaler   = new(Commit)
	_ encoding.BinaryUnmarshaler = new(Commit)
	_ encoding.TextMarshaler     = new(Commit)
	_ encoding.TextUnmarshaler   = new(Commit)
)

var gitCommitTests = []struct {
	name   string
	id     githash.SHA1
	data   string
	parsed *Commit
}{
	{
		name: "RootCommit",
		id:   hashLiteral("aff248747f6a94066967a75e30a5b025816a6aef"),
		data: "tree 58452ad47a5fd3119fb974f9af1818bc88f56857\n" +
			"author Ross Light <ross@zombiezen.com> 1594510150 -0700\n" +
			"committer Ross Light <ross@zombiezen.com> 1594510150 -0700\n" +
			"\n" +
			"Hello World\n",
		parsed: &Commit{
			Tree:       hashLiteral("58452ad47a5fd3119fb974f9af1818bc88f56857"),
			Author:     "Ross Light <ross@zombiezen.com>",
			AuthorTime: time.Unix(1594510150, 0).In(time.FixedZone("-0700", -7*60*60)),
			Committer:  "Ross Light <ross@zombiezen.com>",
			CommitTime: time.Unix(1594510150, 0).In(time.FixedZone("-0700", -7*60*60)),
			Message:    "Hello World\n",
		},
	},
	{
		name: "SingleParentCommit",
		id:   hashLiteral("897fd2d1f07ba5eafffaf6a523d411338d2ffa5f"),
		data: "tree e69c497a490ecaf78f377810e715f0340aa5a10e\n" +
			"parent aff248747f6a94066967a75e30a5b025816a6aef\n" +
			"author Ross Light <ross@zombiezen.com> 1594511739 -0700\n" +
			"committer Ross Light <ross@zombiezen.com> 1594511739 -0700\n" +
			"\n" +
			"Add zv root command\n",
		parsed: &Commit{
			Tree: hashLiteral("e69c497a490ecaf78f377810e715f0340aa5a10e"),
			Parents: []githash.SHA1{
				hashLiteral("aff248747f6a94066967a75e30a5b025816a6aef"),
			},
			Author:     "Ross Light <ross@zombiezen.com>",
			AuthorTime: time.Unix(1594511739, 0).In(time.FixedZone("-0700", -7*60*60)),
			Committer:  "Ross Light <ross@zombiezen.com>",
			CommitTime: time.Unix(1594511739, 0).In(time.FixedZone("-0700", -7*60*60)),
			Message:    "Add zv root command\n",
		},
	},
	{
		name: "Signature",
		id:   hashLiteral("35595b040aac1ecbc21c2bf40e0db227b7740b34"),
		data: "tree 045bad13340b59b9e50c94051200d9f1a729861e\n" +
			"parent b64df08d9368c7a11a4093cc04cf6a307241cf0c\n" +
			"author Ross Light <ross@zombiezen.com> 1595976345 -0700\n" +
			"committer GitHub <noreply@github.com> 1595976345 -0700\n" +
			"gpgsig -----BEGIN PGP SIGNATURE-----\n" +
			" \n" +
			" wsBcBAABCAAQBQJfIKqZCRBK7hj4Ov3rIwAAdHIIACwb+1Dn7I/SdRLPbtCsQ5tX\n" +
			" ea03DZARUh8Z/WCfgwxgCmpy/mdAVXY26CXx4Dm6dweR2tCYA4U98DK9S31fGpSm\n" +
			" V2T8ghIj0iYzWmWYJkTGW3TjIq1elCr+NarH9xfxF+YP1nuF4Z4b/aZ71c/a3YOM\n" +
			" Mjmrb3LQ3uLgExkPOKVbe+ehTrfsjXulkrxOytTwhtXkA0FwXqzYNS0Px3rwUv+2\n" +
			" kXB2DA0YRXVR/+ZTkwYUHPZFM/JNkITJb1rF3nfLa4IYfrLrRsIuAFQlzOJ/KOa4\n" +
			" fHTUTt69O6CFb+p+wIUPmeJD7kuwcDw0JMDH3azqvr6nlLsm5jm8LUkpJbARb7k=\n" +
			" =XV4m\n" +
			" -----END PGP SIGNATURE-----\n" +
			" \n" +
			"\n" +
			"Create NOTES.md",
		parsed: &Commit{
			Tree: hashLiteral("045bad13340b59b9e50c94051200d9f1a729861e"),
			Parents: []githash.SHA1{
				hashLiteral("b64df08d9368c7a11a4093cc04cf6a307241cf0c"),
			},
			Author:     "Ross Light <ross@zombiezen.com>",
			AuthorTime: time.Unix(1595976345, 0).In(time.FixedZone("-0700", -7*60*60)),
			Committer:  "GitHub <noreply@github.com>",
			CommitTime: time.Unix(1595976345, 0).In(time.FixedZone("-0700", -7*60*60)),
			GPGSignature: []byte("-----BEGIN PGP SIGNATURE-----\n" +
				"\n" +
				"wsBcBAABCAAQBQJfIKqZCRBK7hj4Ov3rIwAAdHIIACwb+1Dn7I/SdRLPbtCsQ5tX\n" +
				"ea03DZARUh8Z/WCfgwxgCmpy/mdAVXY26CXx4Dm6dweR2tCYA4U98DK9S31fGpSm\n" +
				"V2T8ghIj0iYzWmWYJkTGW3TjIq1elCr+NarH9xfxF+YP1nuF4Z4b/aZ71c/a3YOM\n" +
				"Mjmrb3LQ3uLgExkPOKVbe+ehTrfsjXulkrxOytTwhtXkA0FwXqzYNS0Px3rwUv+2\n" +
				"kXB2DA0YRXVR/+ZTkwYUHPZFM/JNkITJb1rF3nfLa4IYfrLrRsIuAFQlzOJ/KOa4\n" +
				"fHTUTt69O6CFb+p+wIUPmeJD7kuwcDw0JMDH3azqvr6nlLsm5jm8LUkpJbARb7k=\n" +
				"=XV4m\n" +
				"-----END PGP SIGNATURE-----\n" +
				"\n"),
			Message: "Create NOTES.md",
		},
	},
	{
		name: "Go",
		id:   hashLiteral("7d7c6a97f815e9279d08cfaea7d5efb5e90695a8"),
		data: "tree e06bd601885e16ad3d72c2a8c9b411889b2e478e\n" +
			"author Brian Kernighan <bwk> 80352345 -0500\n" +
			"committer Brian Kernighan <bwk> 80352345 -0500\n" +
			"golang-hg f6182e5abf5eb0c762dddbb18f8854b7e350eaeb\n" +
			"\n" +
			"hello, world\n" +
			"\n" +
			"R=ken\n" +
			"DELTA=7  (7 added, 0 deleted, 0 changed)\n",
		parsed: &Commit{
			Tree:       hashLiteral("e06bd601885e16ad3d72c2a8c9b411889b2e478e"),
			Author:     "Brian Kernighan <bwk>",
			AuthorTime: time.Unix(80352345, 0).In(time.FixedZone("-0500", -5*60*60)),
			Committer:  "Brian Kernighan <bwk>",
			CommitTime: time.Unix(80352345, 0).In(time.FixedZone("-0500", -5*60*60)),
			Extra:      "golang-hg f6182e5abf5eb0c762dddbb18f8854b7e350eaeb",
			Message: "hello, world\n" +
				"\n" +
				"R=ken\n" +
				"DELTA=7  (7 added, 0 deleted, 0 changed)\n",
		},
	},
}

func TestParseCommit(t *testing.T) {
	for _, test := range gitCommitTests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseCommit([]byte(test.data))
			if err != nil {
				t.Error("Error:", err)
			}
			diff := cmp.Diff(test.parsed, got, cmpopts.EquateEmpty())
			if diff != "" {
				t.Errorf("commit (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCommitMarshalText(t *testing.T) {
	for _, test := range gitCommitTests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.parsed.MarshalText()
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(test.data, string(got)); diff != "" {
				t.Errorf("text (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCommitSHA1(t *testing.T) {
	for _, test := range gitCommitTests {
		t.Run(test.name, func(t *testing.T) {
			got := test.parsed.SHA1()
			if !bytes.Equal(got[:], test.id[:]) {
				t.Errorf("sha1() = %x; want %x", got, test.id)
			}
		})
	}
}

func TestUser(t *testing.T) {
	tests := []struct {
		u     User
		name  string
		email string
	}{
		{u: "", name: "", email: ""},
		{u: "<>", name: "", email: ""},
		{u: " <>", name: "", email: ""},
		{u: "Octocat", name: "Octocat", email: ""},
		{u: "Octocat <foo@example.com>", name: "Octocat", email: "foo@example.com"},
		{u: "<foo@example.com>", name: "", email: "foo@example.com"},
		{u: " <foo@example.com>", name: "", email: "foo@example.com"},
		{u: "Octocat :>", name: "Octocat :>", email: ""},
		{u: "Octocat<", name: "Octocat<", email: ""},
		{u: "Octocat> <bar> >", name: "Octocat>", email: "bar"},
	}
	for _, test := range tests {
		if got := test.u.Name(); got != test.name {
			t.Errorf("User(%q).Name() = %q; want %q", test.u, got, test.name)
		}
		if got := test.u.Email(); got != test.email {
			t.Errorf("User(%q).Email() = %q; want %q", test.u, got, test.email)
		}
	}
}

func TestMakeUser(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		want    User
		wantErr bool
	}{
		{name: "", email: "", want: "<>"},
		{name: "Octocat", email: "", want: "Octocat <>"},
		{name: "Octocat", email: "octocat@example.com", want: "Octocat <octocat@example.com>"},
		{name: "", email: "octocat@example.com", want: "<octocat@example.com>"},
		{name: "", email: ">", wantErr: true},
		{name: "", email: "<", want: "<<>"},
		{name: "<", email: "", wantErr: true},
		{name: ">", email: "", want: "> <>"},
		{name: " foo ", email: "", wantErr: true},
	}
	for _, test := range tests {
		got, err := MakeUser(test.name, test.email)
		if got != test.want || (err != nil) != test.wantErr {
			wantErr := "<nil>"
			if test.wantErr {
				wantErr = "<error>"
			}
			t.Errorf("MakeUser(%q, %q) = %q, %v; want %q, %s", test.name, test.email, got, err, test.want, wantErr)
		}
		if test.wantErr {
			continue
		}
		if got := test.want.Name(); got != test.name {
			t.Errorf("User(%q).Name() = %q; want %q", test.want, got, test.name)
		}
		if got := test.want.Email(); got != test.email {
			t.Errorf("User(%q).Email() = %q; want %q", test.want, got, test.email)
		}
	}
}

func TestCommitFieldsCut(t *testing.T) {
	tests := []struct {
		fields   CommitFields
		wantHead CommitFields
		wantTail CommitFields
	}{
		{
			fields:   "",
			wantHead: "",
			wantTail: "",
		},
		{
			fields:   "foo bar",
			wantHead: "foo bar",
			wantTail: "",
		},
		{
			fields:   "foo bar\nbaz quux",
			wantHead: "foo bar",
			wantTail: "baz quux",
		},
		{
			fields:   "foo bar\n baz quux",
			wantHead: "foo bar\n baz quux",
			wantTail: "",
		},
		{
			fields:   "foo bar\n baz quux\ngpgsig",
			wantHead: "foo bar\n baz quux",
			wantTail: "gpgsig",
		},
	}
	for _, test := range tests {
		gotHead, gotTail := test.fields.Cut()
		if gotHead != test.wantHead || gotTail != test.wantTail {
			t.Errorf("CommitFields(%q).Cut() = %q, %q; want %q, %q", test.fields, gotHead, gotTail, test.wantHead, test.wantTail)
		}
	}
}

func TestCommitFieldsFirst(t *testing.T) {
	tests := []struct {
		fields    CommitFields
		wantKey   string
		wantValue string
	}{
		{
			fields:    "",
			wantKey:   "",
			wantValue: "",
		},
		{
			fields:    "foo bar",
			wantKey:   "foo",
			wantValue: "bar",
		},
		{
			fields:    "foo bar\nbaz quux",
			wantKey:   "foo",
			wantValue: "bar",
		},
		{
			fields:    "foo bar\n baz quux",
			wantKey:   "foo",
			wantValue: "bar\nbaz quux",
		},
		{
			fields:    "foo bar\n baz quux\ngpgsig",
			wantKey:   "foo",
			wantValue: "bar\nbaz quux",
		},
		{
			fields:    "foo\n bar baz\ngpgsig",
			wantKey:   "foo",
			wantValue: "\nbar baz",
		},
	}
	for _, test := range tests {
		gotKey, gotValue := test.fields.First()
		if gotKey != test.wantKey || gotValue != test.wantValue {
			t.Errorf("CommitFields(%q).First() = %q, %q; want %q, %q", test.fields, gotKey, gotValue, test.wantKey, test.wantValue)
		}
	}
}

func TestCommitFieldsGet(t *testing.T) {
	tests := []struct {
		fields CommitFields
		key    string
		want   string
	}{
		{
			fields: "",
			key:    "foo",
			want:   "",
		},
		{
			fields: "foo bar",
			key:    "foo",
			want:   "bar",
		},
		{
			fields: "hello world",
			key:    "foo",
			want:   "",
		},
		{
			fields: "hello world\nfoo bar",
			key:    "foo",
			want:   "bar",
		},
		{
			fields: "foo bar\nhello world",
			key:    "foo",
			want:   "bar",
		},
		{
			fields: "foo bar\n continuation line\nhello world",
			key:    "foo",
			want:   "bar\ncontinuation line",
		},
		{
			fields: "foo bar\n continuation line\nhello world",
			key:    " continuation",
			want:   "",
		},
	}
	for _, test := range tests {
		got := test.fields.Get(test.key)
		if got != test.want {
			t.Errorf("CommitFields(%q).Get(%q) = %q; want %q", test.fields, test.key, got, test.want)
		}
	}
}
