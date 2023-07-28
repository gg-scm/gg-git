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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"gg-scm.io/pkg/git/internal/giturl"
)

// CloneOptions specifies the command-line options for `git clone`.
type CloneOptions struct {
	// Dir is the path to a directory to clone into. If empty, the final path
	// component of the URL is used, removing any ".git" suffix.
	Dir string

	// Progress receives the stderr of the `git clone` subprocess if not nil.
	Progress io.Writer

	// HeadBranch sets the branch that the new repository's HEAD will point to.
	// If empty, uses the same HEAD as the remote repository.
	HeadBranch string
	// RemoteName sets the name of the new remote. Defaults to "origin" if empty.
	RemoteName string

	// If Depth is greater than zero, it limits the depth of the commits cloned.
	// It is mutually exclusive with Since.
	Depth int
	// Since requests that the shallow clone should be cut at a specific time.
	// It is mutually exclusive with Depth.
	Since time.Time
	// ShallowExclude is a set of revisions that the remote will exclude from
	// the packfile. Unlike Have, the remote will send any needed trees and
	// blobs even if they are shared with the revisions in ShallowExclude.
	// It is mutually exclusive with Depth, but not Since. This is only supported
	// by the remote if it has PullCapShallowExclude.
	ShallowExclude []string
}

// Clone clones the repository at the given URL and checks out HEAD.
func (g *Git) Clone(ctx context.Context, u *url.URL, opts CloneOptions) error {
	if err := g.clone(ctx, "", u, opts); err != nil {
		return fmt.Errorf("git clone %v: %w", u, err)
	}
	return nil
}

// CloneBare clones the repository at the given URL without creating a working copy.
func (g *Git) CloneBare(ctx context.Context, u *url.URL, opts CloneOptions) error {
	if err := g.clone(ctx, "--bare", u, opts); err != nil {
		return fmt.Errorf("git clone --bare %v: %w", u, err)
	}
	return nil
}

func (g *Git) clone(ctx context.Context, mode string, u *url.URL, opts CloneOptions) error {
	var args []string
	args = append(args, "clone")
	if opts.Progress == nil {
		args = append(args, "--quiet")
	} else {
		args = append(args, "--progress")
	}
	if mode != "" {
		args = append(args, mode)
	}
	if opts.HeadBranch != "" {
		args = append(args, "--branch="+opts.HeadBranch)
	}
	if opts.RemoteName != "" {
		args = append(args, "--origin="+opts.RemoteName)
	}
	if opts.Depth > 0 {
		args = append(args, fmt.Sprintf("--depth=%d", opts.Depth))
	}
	if !opts.Since.IsZero() {
		args = append(args, fmt.Sprintf("--shallow-since=%d", opts.Since.Unix()))
	}
	for _, rev := range opts.ShallowExclude {
		args = append(args, "--shallow-exclude="+rev)
	}
	args = append(args, "--", u.String())
	if opts.Dir != "" {
		args = append(args, opts.Dir)
	}
	return g.runner.RunGit(ctx, &Invocation{
		Args:   args,
		Dir:    g.dir,
		Stderr: opts.Progress,
	})
}

// IterateRemoteRefsOptions specifies filters for [Git.IterateRemoteRefs].
type IterateRemoteRefsOptions struct {
	// If IncludeHead is true, the HEAD ref is included.
	IncludeHead bool
	// LimitToBranches limits the refs to those starting with "refs/heads/".
	// This is additive with IncludeHead and LimitToTags.
	LimitToBranches bool
	// LimitToTags limits the refs to those starting with "refs/tags/".
	// This is additive with IncludeHead and LimitToBranches.
	LimitToTags bool
	// If DereferenceTags is true,
	// then the iterator will also produce refs for which [RefIterator.IsDereference] reports true
	// that have the object hash of the tag object refers to.
	DereferenceTags bool
}

// ListRemoteRefs lists all of the refs in a remote repository.
// remote may be a URL or the name of a remote.
//
// This function may block on user input if the remote requires
// credentials.
//
// Deprecated: This method will return an error on repositories with many branches (>100K).
// Use [Git.IterateRemoteRefs] instead.
func (g *Git) ListRemoteRefs(ctx context.Context, remote string) (map[Ref]Hash, error) {
	return parseRefs(g.IterateRemoteRefs(ctx, remote, IterateRemoteRefsOptions{
		IncludeHead: true,
	}))
}

// IterateRemoteRefs starts listing all of the refs in a remote repository.
// remote may be a URL or the name of a remote.
//
// The iterator may block on user input if the remote requires credentials.
func (g *Git) IterateRemoteRefs(ctx context.Context, remote string, opts IterateRemoteRefsOptions) *RefIterator {
	// TODO(someday): Add tests.

	errPrefix := fmt.Sprintf("git ls-remote %q", remote)
	args := []string{"ls-remote", "--quiet"}
	if !opts.IncludeHead && !opts.DereferenceTags {
		args = append(args, "--refs")
	}
	if opts.LimitToBranches {
		args = append(args, "--heads")
	}
	if opts.LimitToTags {
		args = append(args, "--tags")
	}
	args = append(args, "--", remote)

	ctx, cancel := context.WithCancel(ctx)
	stderr := new(bytes.Buffer)
	pipe, err := StartPipe(ctx, g.runner, &Invocation{
		Args:   args,
		Dir:    g.dir,
		Stderr: &limitWriter{w: stderr, n: errorOutputLimit},
	})
	if err != nil {
		cancel()
		return &RefIterator{
			scanErr:  fmt.Errorf("%s: %w", errPrefix, err),
			scanDone: true,
		}
	}
	return &RefIterator{
		scanner:      bufio.NewScanner(pipe),
		stderr:       stderr,
		cancelFunc:   cancel,
		closer:       pipe,
		errPrefix:    errPrefix,
		ignoreDerefs: !opts.DereferenceTags,
		ignoreHead:   !opts.IncludeHead,
	}
}

// A FetchRefspec specifies a mapping from remote refs to local refs.
type FetchRefspec string

// String returns the refspec as a string.
func (spec FetchRefspec) String() string {
	return string(spec)
}

// Parse parses the refspec into its parts.
func (spec FetchRefspec) Parse() (src, dst RefPattern, plus bool) {
	plus = strings.HasPrefix(string(spec), "+")
	s := string(spec)
	if plus {
		s = s[1:]
	}
	if i := strings.IndexByte(s, ':'); i != -1 {
		return RefPattern(s[:i]), RefPattern(s[i+1:]), plus
	}
	if strings.HasPrefix(s, "tag ") {
		name := s[len("tag "):]
		return RefPattern("refs/tags/" + name), RefPattern("refs/tags/" + name), plus
	}
	return RefPattern(s), "", plus
}

// Map maps a remote ref into a local ref. If there is no mapping, then
// Map returns an empty Ref.
func (spec FetchRefspec) Map(remote Ref) Ref {
	srcPattern, dstPattern, _ := spec.Parse()
	suffix, ok := srcPattern.Match(remote)
	if !ok {
		return ""
	}
	if prefix, ok := dstPattern.Prefix(); ok {
		return Ref(prefix + suffix)
	}
	return Ref(dstPattern)
}

// A RefPattern is a part of a refspec. It may be either a literal
// suffix match (e.g. "main" matches "refs/head/main"), or the last
// component may be a wildcard ('*'), which indicates a prefix match.
type RefPattern string

// String returns the pattern string.
func (pat RefPattern) String() string {
	return string(pat)
}

// Prefix returns the prefix before the wildcard if it's a wildcard
// pattern. Otherwise it returns "", false.
func (pat RefPattern) Prefix() (_ string, ok bool) {
	if pat == "*" {
		return "", true
	}
	const wildcard = "/*"
	if strings.HasSuffix(string(pat), wildcard) && len(pat) > len(wildcard) {
		return string(pat[:len(pat)-1]), true
	}
	return "", false
}

// Match reports whether a ref matches the pattern.
// If the pattern is a prefix match, then suffix is the string matched by the wildcard.
func (pat RefPattern) Match(ref Ref) (suffix string, ok bool) {
	prefix, ok := pat.Prefix()
	if ok {
		if !strings.HasPrefix(string(ref), prefix) {
			return "", false
		}
		return string(ref[len(prefix):]), true
	}
	return "", string(ref) == string(pat) || strings.HasSuffix(string(ref), string("/"+pat))
}

// ParseURL parses a Git remote URL, including the alternative SCP syntax.
// See git-fetch(1) for details.
func ParseURL(urlstr string) (*url.URL, error) {
	return giturl.Parse(urlstr)
}

// URLFromPath converts a filesystem path into a URL. If it's a relative path,
// then it returns a path-only URL.
func URLFromPath(path string) *url.URL {
	return giturl.FromPath(path)
}
