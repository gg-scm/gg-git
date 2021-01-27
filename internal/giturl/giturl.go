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
	"errors"
	"net/url"
	"path/filepath"
	"strings"
)

// Parse parses a Git remote URL, including the alternative SCP syntax.
// See git-fetch(1) for details.
func Parse(urlstr string) (*url.URL, error) {
	if urlstr == "" {
		return nil, errors.New("parse git url: empty string")
	}
	if i := strings.IndexAny(urlstr, ":/"); i != -1 {
		if tail := urlstr[i:]; !strings.HasPrefix(tail, "/") &&
			!strings.HasPrefix(tail, "://") &&
			!strings.HasPrefix(tail, "::") {
			urlstr = "ssh://" + urlstr[:i] + "/" + strings.TrimPrefix(tail[1:], "/")
		}
	}
	return url.Parse(urlstr)
}

// FromPath converts a filesystem path into a URL. If it's a relative path, then
// it returns a path-only URL.
func FromPath(path string) *url.URL {
	if !filepath.IsAbs(path) {
		return &url.URL{Path: filepath.ToSlash(path)}
	}
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		// For Windows paths that start with "C:/".
		path = "/" + path
	}
	return &url.URL{
		Scheme: "file",
		Path:   path,
	}
}
