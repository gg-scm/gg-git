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
	_ encoding.TextUnmarshaler = new(Tag)
	_ encoding.TextMarshaler   = new(Tag)
)

var gitTagTests = []struct {
	name   string
	id     githash.SHA1
	data   string
	parsed *Tag
}{
	{
		name: "Version072",
		id:   hashLiteral("173b8be873eddc95bebd2452dd38afa04cd64c90"),
		data: "object b90a244ea5b7a6792cb09132aa0887a807d000f2\n" +
			"type commit\n" +
			"tag v0.7.2\n" +
			"tagger Ross Light <ross@zombiezen.com> 1601844945 -0700\n" +
			"\n" +
			"Release version 0.7.2\n",
		parsed: &Tag{
			ObjectID:   hashLiteral("b90a244ea5b7a6792cb09132aa0887a807d000f2"),
			ObjectType: TypeCommit,
			Name:       "v0.7.2",
			Tagger:     "Ross Light <ross@zombiezen.com>",
			Time:       time.Unix(1601844945, 0).In(time.FixedZone("-0700", -7*60*60)),
			Message:    "Release version 0.7.2\n",
		},
	},
}

func TestParseTag(t *testing.T) {
	for _, test := range gitTagTests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseTag([]byte(test.data))
			if err != nil {
				t.Error("Error:", err)
			}
			diff := cmp.Diff(test.parsed, got, cmpopts.EquateEmpty())
			if diff != "" {
				t.Errorf("tag (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTagMarshalText(t *testing.T) {
	for _, test := range gitTagTests {
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

func TestTagSHA1(t *testing.T) {
	for _, test := range gitTagTests {
		t.Run(test.name, func(t *testing.T) {
			got := test.parsed.SHA1()
			if !bytes.Equal(got[:], test.id[:]) {
				t.Errorf("sha1() = %x; want %x", got, test.id)
			}
		})
	}
}
