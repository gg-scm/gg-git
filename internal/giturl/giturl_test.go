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

package giturl

import (
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"
)

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
		got, err := Parse(test.urlstr)
		equal := cmp.Equal(test.want, got,
			cmp.Comparer(func(u1, u2 *url.Userinfo) bool {
				pw1, ok1 := u1.Password()
				pw2, ok2 := u2.Password()
				return u1.Username() == u2.Username() && pw1 == pw2 && ok1 == ok2
			}),
		)
		if test.wantErr {
			if err == nil {
				t.Errorf("Parse(%q) = %v, <nil>; want _, <error>", test.urlstr, got)
			} else {
				t.Logf("Parse(%q) = _, %v", test.urlstr, err)
			}
		} else if !equal {
			t.Errorf("Parse(%q) = %v, %v; want %v, <nil>", test.urlstr, got, err, test.want)
		}
	}
}
