// Copyright 2023 The gg Authors
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

package gitglob

import (
	"path"
	"testing"
)

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    bool
		err     bool
	}{
		{"foo", "foo", true, false},
		{"foo", "bar/foo", false, false},
		{"foo", "xfoo", false, false},
		{"foo", "bar/xfoo", false, false},
		{"*.txt", "foo.txt", true, false},
		{"*.txt", "foo.jpg", false, false},
		{"a*", "a", true, false},
		{"a*", "abc", true, false},
		{"a*", "ab/c", false, false},
		{"a*b*c*d*e*/f", "axbxcxdxe/f", true, false},
		{"a*b*c*d*e*/f", "axbxcxdxexxx/f", true, false},
		{"a*b*c*d*e*/f", "axbxcxdxe/xxx/f", false, false},
		{"a*b*c*d*e*/f", "axbxcxdxexxx/fff", false, false},
		{"ab[c]", "abc", true, false},
		{"ab[b-d]", "abc", true, false},
		{"ab[e-g]", "abc", false, false},
		{"[\\]a]", "]", true, false},
		{"[\\-]", "-", true, false},
		{"[x\\-]", "x", true, false},
		{"[x\\-]", "-", true, false},
		{"[x\\-]", "z", false, false},
		{"[]a]", "]", false, true},
		{"[-]", "-", false, true},
		{"[x-]", "x", false, true},
		{"[-x]", "x", false, true},
		{"[a-b-c]", "a", false, true},
		{"a?b", "a☺b", true, false},

		{"**/foo", "foo", true, false},
		{"**/foo", "bar/foo", true, false},
		{"**/foo", "xfoo", false, false},
		{"**/foo", "bar/xfoo", false, false},
		{"abc/**", "abc/x", true, false},
		{"abc/**", "x/abc/y", false, false},
		{"a/**/b", "a/b", true, false},
		{"a/**/b", "a/x/b", true, false},
		{"a/**/b", "a/x/y/b", true, false},
		{"a/**/b", "a/b/c", false, false},
		{"foo//bar", "foo/bar", false, false},
	}
	for _, test := range tests {
		got, err := Match(test.pattern, test.name)
		if got != test.want || (err != nil) != test.err {
			errString := "<nil>"
			if test.err {
				errString = "<error>"
			}
			t.Errorf("Match(%q, %q) = %t, %v; want %t, %s",
				test.pattern, test.name, got, err, test.want, errString)
		}
	}
}

func BenchmarkMatch(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Match("a/**/b", "a/x/b")
	}
}

func BenchmarkGlobMatchString(b *testing.B) {
	g, err := Compile("a/**/b")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		g.MatchString("a/x/b")
	}
}

func BenchmarkPathMatch(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		path.Match("a/*/b", "a/x/b")
	}
}
