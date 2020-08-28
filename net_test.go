// Copyright 2019 The gg Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package git

import (
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestFetchRefspecParse(t *testing.T) {
	tests := []struct {
		spec FetchRefspec
		src  RefPattern
		dst  RefPattern
		plus bool
	}{
		{spec: "", src: "", dst: "", plus: false},
		{spec: "foo", src: "foo", dst: "", plus: false},
		{spec: "foo:", src: "foo", dst: "", plus: false},
		{spec: "foo:bar", src: "foo", dst: "bar", plus: false},
		{spec: "refs/heads/*:refs/remotes/origin/*", src: "refs/heads/*", dst: "refs/remotes/origin/*", plus: false},
		{spec: "tag baz", src: "refs/tags/baz", dst: "refs/tags/baz", plus: false},
		{spec: "+", src: "", dst: "", plus: true},
		{spec: "+foo", src: "foo", dst: "", plus: true},
		{spec: "+foo:", src: "foo", dst: "", plus: true},
		{spec: "+foo:bar", src: "foo", dst: "bar", plus: true},
		{spec: "+refs/heads/*:refs/remotes/origin/*", src: "refs/heads/*", dst: "refs/remotes/origin/*", plus: true},
		{spec: "+tag baz", src: "refs/tags/baz", dst: "refs/tags/baz", plus: true},
	}
	for _, test := range tests {
		src, dst, plus := test.spec.Parse()
		if src != test.src || dst != test.dst || plus != test.plus {
			t.Errorf("FetchRefspec(%q).Parse() = %q, %q, %t; want %q, %q, %t", test.spec, src, dst, plus, test.src, test.dst, test.plus)
		}
	}
}

func TestFetchRefspecMap(t *testing.T) {
	tests := []struct {
		spec   FetchRefspec
		local  Ref
		remote Ref
	}{
		{
			spec:   "+refs/heads/*:refs/remotes/origin/*",
			local:  "refs/heads/main",
			remote: "refs/remotes/origin/main",
		},
		{
			spec:   "+refs/heads/*:refs/remotes/origin/*",
			local:  "refs/tags/v1.0.0",
			remote: "",
		},
		{
			spec:   "+refs/heads/*:refs/special-remote",
			local:  "refs/heads/main",
			remote: "refs/special-remote",
		},
		{
			spec:   "+main:refs/special-remote",
			local:  "refs/heads/main",
			remote: "refs/special-remote",
		},
		{
			spec:   "+main:refs/special-remote",
			local:  "refs/heads/feature",
			remote: "",
		},
	}
	for _, test := range tests {
		if remote := test.spec.Map(test.local); remote != test.remote {
			t.Errorf("FetchRefspec(%q).Map(%q) = %q; want %q", test.spec, test.local, remote, test.remote)
		}
	}
}

func TestRefPatternPrefix(t *testing.T) {
	tests := []struct {
		pat    RefPattern
		prefix string
		ok     bool
	}{
		{pat: "", ok: false},
		{pat: "*", ok: true, prefix: ""},
		{pat: "/*", ok: false, prefix: ""},
		{pat: "er", ok: false},
		{pat: "main", ok: false},
		{pat: "/main", ok: false},
		{pat: "heads/main", ok: false},
		{pat: "refs/heads/main", ok: false},
		{pat: "refs", ok: false},
		{pat: "refs/qa*", ok: false},
		{pat: "refs/*", ok: true, prefix: "refs/"},
		{pat: "refs/heads/*", ok: true, prefix: "refs/heads/"},
	}
	for _, test := range tests {
		prefix, ok := test.pat.Prefix()
		if prefix != test.prefix || ok != test.ok {
			t.Errorf("RefPattern(%q).Prefix() = %q, %t; want %q, %t", test.pat, prefix, ok, test.prefix, test.ok)
		}
	}
}

func TestRefPatternMatches(t *testing.T) {
	tests := []struct {
		pat    RefPattern
		ref    Ref
		suffix string
		ok     bool
	}{
		{pat: "", ref: "refs/heads/main", ok: false},
		{pat: "*", ref: "refs/heads/main", ok: true, suffix: "refs/heads/main"},
		{pat: "er", ref: "refs/heads/main", ok: false},
		{pat: "main", ref: "refs/heads/main", ok: true},
		{pat: "/main", ref: "refs/heads/main", ok: false},
		{pat: "heads/main", ref: "refs/heads/main", ok: true},
		{pat: "refs/heads/main", ref: "refs/heads/main", ok: true},
		{pat: "refs", ref: "refs/heads/main", ok: false},
		{pat: "refs/heads/*", ref: "refs/heads/main", ok: true, suffix: "main"},
		{pat: "refs/*", ref: "refs/heads/main", ok: true, suffix: "heads/main"},
	}
	for _, test := range tests {
		suffix, ok := test.pat.Match(test.ref)
		if suffix != test.suffix || ok != test.ok {
			t.Errorf("RefPattern(%q).Match(Ref(%q)) = %q, %t; want %q, %t", test.pat, test.ref, suffix, ok, test.suffix, test.ok)
		}
	}
}

func TestParseURL(t *testing.T) {
	tests := []struct {
		urlstr  string
		want    *url.URL
		wantErr bool
	}{
		{
			urlstr:  "",
			wantErr: true,
		},

		// SSH URLs
		{
			urlstr: "ssh://user@host.xz:22/path/to/repo.git/",
			want: &url.URL{
				Scheme: "ssh",
				User:   url.User("user"),
				Host:   "host.xz:22",
				Path:   "/path/to/repo.git/",
			},
		},
		{
			urlstr: "ssh://host.xz/path/to/repo.git/",
			want: &url.URL{
				Scheme: "ssh",
				Host:   "host.xz",
				Path:   "/path/to/repo.git/",
			},
		},
		{
			urlstr: "user@host.xz:path/to/repo.git/",
			want: &url.URL{
				Scheme: "ssh",
				User:   url.User("user"),
				Host:   "host.xz",
				Path:   "/path/to/repo.git/",
			},
		},
		{
			urlstr: "host.xz:path/to/repo.git/",
			want: &url.URL{
				Scheme: "ssh",
				Host:   "host.xz",
				Path:   "/path/to/repo.git/",
			},
		},
		{
			urlstr: "user@host.xz:/~other/path/to/repo.git/",
			want: &url.URL{
				Scheme: "ssh",
				User:   url.User("user"),
				Host:   "host.xz",
				Path:   "/~other/path/to/repo.git/",
			},
		},
		{
			urlstr: "host.xz:/~/path/to/repo.git/",
			want: &url.URL{
				Scheme: "ssh",
				Host:   "host.xz",
				Path:   "/~/path/to/repo.git/",
			},
		},
		{
			urlstr: "foo:bar",
			want: &url.URL{
				Scheme: "ssh",
				Host:   "foo",
				Path:   "/bar",
			},
		},

		// Git URLs
		{
			urlstr: "git://host.xz:9418/path/to/repo.git/",
			want: &url.URL{
				Scheme: "git",
				Host:   "host.xz:9418",
				Path:   "/path/to/repo.git/",
			},
		},
		{
			urlstr: "git://host.xz/path/to/repo.git/",
			want: &url.URL{
				Scheme: "git",
				Host:   "host.xz",
				Path:   "/path/to/repo.git/",
			},
		},

		// HTTP URLs
		{
			urlstr: "http://host.xz:80/path/to/repo.git/",
			want: &url.URL{
				Scheme: "http",
				Host:   "host.xz:80",
				Path:   "/path/to/repo.git/",
			},
		},
		{
			urlstr: "http://host.xz/path/to/repo.git/",
			want: &url.URL{
				Scheme: "http",
				Host:   "host.xz",
				Path:   "/path/to/repo.git/",
			},
		},
		{
			urlstr: "https://host.xz:443/path/to/repo.git/",
			want: &url.URL{
				Scheme: "https",
				Host:   "host.xz:443",
				Path:   "/path/to/repo.git/",
			},
		},
		{
			urlstr: "https://host.xz/path/to/repo.git/",
			want: &url.URL{
				Scheme: "https",
				Host:   "host.xz",
				Path:   "/path/to/repo.git/",
			},
		},
		{
			urlstr: "https://user@host.xz/path/to/repo.git/",
			want: &url.URL{
				Scheme: "https",
				User:   url.User("user"),
				Host:   "host.xz",
				Path:   "/path/to/repo.git/",
			},
		},
		{
			urlstr: "https://user:password@host.xz/path/to/repo.git/",
			want: &url.URL{
				Scheme: "https",
				User:   url.UserPassword("user", "password"),
				Host:   "host.xz",
				Path:   "/path/to/repo.git/",
			},
		},

		// FTP URLs
		{
			urlstr: "ftp://host.xz:21/path/to/repo.git/",
			want: &url.URL{
				Scheme: "ftp",
				Host:   "host.xz:21",
				Path:   "/path/to/repo.git/",
			},
		},
		{
			urlstr: "ftp://host.xz/path/to/repo.git/",
			want: &url.URL{
				Scheme: "ftp",
				Host:   "host.xz",
				Path:   "/path/to/repo.git/",
			},
		},
		{
			urlstr: "ftps://host.xz:21/path/to/repo.git/",
			want: &url.URL{
				Scheme: "ftps",
				Host:   "host.xz:21",
				Path:   "/path/to/repo.git/",
			},
		},
		{
			urlstr: "ftps://host.xz/path/to/repo.git/",
			want: &url.URL{
				Scheme: "ftps",
				Host:   "host.xz",
				Path:   "/path/to/repo.git/",
			},
		},

		// File URLs
		{
			urlstr: "foo",
			want: &url.URL{
				Path: "foo",
			},
		},
		{
			urlstr: "./foo:bar",
			want: &url.URL{
				Path: "./foo:bar",
			},
		},
		{
			urlstr: "/path/to/repo.git/",
			want: &url.URL{
				Path: "/path/to/repo.git/",
			},
		},
		{
			urlstr: "file:///C:/path/to/repo.git/",
			want: &url.URL{
				Scheme: "file",
				Path:   "/C:/path/to/repo.git/",
			},
		},
		{
			urlstr: "file:///path/to/repo.git/",
			want: &url.URL{
				Scheme: "file",
				Path:   "/path/to/repo.git/",
			},
		},

		// Other transports
		{
			urlstr: "transport::address",
			want: &url.URL{
				Scheme: "transport",
				Opaque: ":address",
			},
		},
		{
			urlstr: "transport://address",
			want: &url.URL{
				Scheme: "transport",
				Host:   "address",
			},
		},
	}
	for _, test := range tests {
		got, err := ParseURL(test.urlstr)
		equal := cmp.Equal(test.want, got,
			cmp.Comparer(func(u1, u2 *url.Userinfo) bool {
				pw1, ok1 := u1.Password()
				pw2, ok2 := u2.Password()
				return u1.Username() == u2.Username() && pw1 == pw2 && ok1 == ok2
			}),
		)
		if test.wantErr {
			if err == nil {
				t.Errorf("ParseURL(%q) = %v, <nil>; want _, <error>", test.urlstr, got)
			} else {
				t.Logf("ParseURL(%q) = _, %v", test.urlstr, err)
			}
		} else if !equal {
			t.Errorf("ParseURL(%q) = %v, %v; want %v, <nil>", test.urlstr, got, err, test.want)
		}
	}
}
