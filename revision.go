// Copyright 2018 The gg Authors
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
	"errors"
	"fmt"
	"io"
	"strings"

	"gg-scm.io/pkg/git/githash"
)

// A Hash is the SHA-1 hash of a Git object.
type Hash = githash.SHA1

// ParseHash parses a hex-encoded hash. It is the same as calling UnmarshalText
// on a new Hash.
func ParseHash(s string) (Hash, error) {
	return githash.ParseSHA1(s)
}

// A Ref is a Git reference to a commit.
type Ref = githash.Ref

// Top-level refs.
const (
	// Head names the commit on which the changes in the working tree
	// are based.
	Head = githash.Head

	// FetchHead records the branch which was fetched from a remote
	// repository with the last git fetch invocation.
	FetchHead = githash.FetchHead
)

// BranchRef returns a ref for the given branch name.
func BranchRef(b string) Ref {
	return githash.BranchRef(b)
}

// TagRef returns a ref for the given tag name.
func TagRef(t string) Ref {
	return githash.TagRef(t)
}

// Head returns the working copy's branch revision. If the branch does
// not point to a valid commit (such as when the repository is first
// created), then Head returns an error.
func (g *Git) Head(ctx context.Context) (*Rev, error) {
	return g.ParseRev(ctx, Head.String())
}

// HeadRef returns the working copy's branch. If the working copy is in
// detached HEAD state, then HeadRef returns an empty string and no
// error. The ref may not point to a valid commit.
func (g *Git) HeadRef(ctx context.Context) (Ref, error) {
	const errPrefix = "head ref"
	stdout, err := g.output(ctx, errPrefix, []string{"symbolic-ref", "--quiet", "HEAD"})
	if err != nil {
		if exitCode(err) == 1 {
			// Not a symbolic ref: detached HEAD.
			return "", nil
		}
		return "", err
	}
	name, err := oneLine(stdout)
	if err != nil {
		return "", fmt.Errorf("%s: %w", errPrefix, err)
	}
	return Ref(name), nil
}

// ParseRev parses a revision.
func (g *Git) ParseRev(ctx context.Context, refspec string) (*Rev, error) {
	errPrefix := fmt.Sprintf("parse revision %q", refspec)
	if err := validateRev(refspec); err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}

	out, err := g.output(ctx, errPrefix, []string{"rev-parse", "-q", "--verify", "--revs-only", refspec + "^0"})
	if err != nil {
		return nil, err
	}
	commitHex, err := oneLine(out)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	h, err := ParseHash(commitHex)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}

	out, err = g.output(ctx, errPrefix, []string{"rev-parse", "-q", "--verify", "--revs-only", "--symbolic-full-name", refspec})
	if err != nil {
		return nil, err
	}
	if out == "" {
		// No associated ref name, but is a valid commit.
		return &Rev{Commit: h}, nil
	}
	refName, err := oneLine(out)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	return &Rev{
		Commit: h,
		Ref:    Ref(refName),
	}, nil
}

// ListRefs lists all of the refs in the repository with tags dereferenced.
//
// Deprecated: This method will return an error on repositories with many branches (>100K).
// Use [Git.IterateRefs] instead.
func (g *Git) ListRefs(ctx context.Context) (map[Ref]Hash, error) {
	return parseRefs(g.IterateRefs(ctx, IterateRefsOptions{
		IncludeHead:     true,
		DereferenceTags: true,
	}))
}

// ListRefsVerbatim lists all of the refs in the repository.
// Tags will not be dereferenced.
//
// Deprecated: This method will return an error on repositories with many branches (>100K).
// Use [Git.IterateRefs] instead.
func (g *Git) ListRefsVerbatim(ctx context.Context) (map[Ref]Hash, error) {
	return parseRefs(g.IterateRefs(ctx, IterateRefsOptions{
		IncludeHead: true,
	}))
}

func parseRefs(iter *RefIterator) (map[Ref]Hash, error) {
	defer iter.Close()

	const maxCount = 164000 // approximately 10 MiB, assuming 64 bytes per record

	refs := make(map[Ref]Hash)
	tags := make(map[Ref]bool)
	for iter.Next() {
		if iter.IsDereference() {
			// Dereferenced tag. This takes precedence over the previous hash stored in the map.
			if tags[iter.Ref()] {
				return refs, fmt.Errorf("parse refs: multiple hashes found for tag %v", iter.Ref())
			}
			tags[iter.Ref()] = true
		} else if _, exists := refs[iter.Ref()]; exists {
			return refs, fmt.Errorf("parse refs: multiple hashes found for %v", iter.Ref())
		} else if len(refs) >= maxCount {
			return refs, fmt.Errorf("parse refs: too many refs")
		}
		refs[iter.Ref()] = iter.ObjectSHA1()
	}
	return refs, iter.Close()
}

// IterateRefsOptions specifies filters for [Git.IterateRefs].
type IterateRefsOptions struct {
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

// IterateRefs starts listing all of the refs in the repository.
func (g *Git) IterateRefs(ctx context.Context, opts IterateRefsOptions) *RefIterator {
	const errPrefix = "git show-ref"
	args := []string{"show-ref"}
	if opts.IncludeHead {
		args = append(args, "--head")
	}
	if opts.LimitToBranches {
		args = append(args, "--heads")
	}
	if opts.LimitToTags {
		args = append(args, "--tags")
	}
	if opts.DereferenceTags {
		args = append(args, "--dereference")
	}

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
		ignoreExit1:  true,
	}
}

// RefIterator is an open handle to a Git subprocess that lists refs.
// Closing the iterator stops the subprocess.
type RefIterator struct {
	scanner      *bufio.Scanner
	stderr       *bytes.Buffer
	cancelFunc   context.CancelFunc
	closer       io.Closer
	errPrefix    string
	ignoreDerefs bool
	ignoreHead   bool
	ignoreExit1  bool

	scanErr    error
	scanDone   bool
	hasResults bool
	ref        Ref
	hash       githash.SHA1
	deref      bool
}

// Next attempts to scan the next ref and reports whether one exists.
func (iter *RefIterator) Next() bool {
	if iter.scanDone {
		return false
	}
	err := iter.next()
	if err != nil {
		iter.cancel()
		iter.scanErr = err
		if errors.Is(err, io.EOF) {
			iter.scanErr = nil
		}
		return false
	}
	iter.hasResults = true
	return true
}

func (iter *RefIterator) next() error {
	isSpace := func(c rune) bool { return c == ' ' || c == '\t' }

	for iter.scanner.Scan() {
		line := iter.scanner.Bytes()
		sp := bytes.IndexAny(line, " \t")
		if sp == -1 {
			return fmt.Errorf("parse refs: could not parse line %q", line)
		}
		refBytes := bytes.TrimLeftFunc(line[sp+1:], isSpace)
		const derefSuffix = "^{}"
		iter.deref = len(refBytes) >= len(derefSuffix) &&
			string(refBytes[len(refBytes)-len(derefSuffix):]) == derefSuffix
		if iter.deref {
			refBytes = refBytes[:len(refBytes)-len(derefSuffix)]
		}
		ref := Ref(refBytes)
		if (!iter.deref || !iter.ignoreDerefs) && (ref != Head || !iter.ignoreHead) {
			if err := iter.hash.UnmarshalText(line[:sp]); err != nil {
				return fmt.Errorf("parse refs: hash of ref %q: %w", line[sp+1:], err)
			}
			iter.ref = ref
			return nil
		}
	}

	if err := iter.scanner.Err(); err != nil {
		return err
	}
	// Reached successful end. Wait for subprocess to exit.
	if err := iter.close(); err != nil {
		return err
	}
	return io.EOF
}

// Ref returns the current ref.
// [RefIterator.Next] must be called at least once before calling Ref.
func (iter *RefIterator) Ref() Ref {
	return iter.ref
}

// ObjectSHA1 returns the SHA-1 hash of the Git object
// the current ref refers to.
// [RefIterator.Next] must be called at least once before calling ObjectSHA1.
func (iter *RefIterator) ObjectSHA1() githash.SHA1 {
	return iter.hash
}

// IsDereference reports whether the value of [RefIterator.ObjectSHA1]
// represents the target of a tag object.
func (iter *RefIterator) IsDereference() bool {
	return iter.deref
}

// Close ends the Git subprocess and waits for it to finish.
// Close returns an error if [RefIterator.Next] returned false
// due to a parse failure.
// Subsequent calls to Close will no-op and return the same error.
func (iter *RefIterator) Close() error {
	iter.cancel()
	iter.close() // Ignore error, since it's from interrupting.
	return iter.scanErr
}

func (iter *RefIterator) cancel() {
	if iter.cancelFunc != nil {
		iter.cancelFunc()
	}
	iter.scanner = nil
	iter.scanDone = true
	iter.ref = ""
	iter.hash = Hash{}
	iter.deref = false
}

func (iter *RefIterator) close() error {
	if iter.closer == nil {
		return nil
	}
	err := iter.closer.Close()
	iter.closer = nil
	if err != nil && !(iter.ignoreExit1 && !iter.hasResults && exitCode(err) == 1) {
		return commandError(iter.errPrefix, err, iter.stderr.Bytes())
	}
	return nil
}

// A RefMutation describes an operation to perform on a ref. The zero value is
// a no-op.
type RefMutation struct {
	command  string
	newvalue string
	oldvalue string
}

const refZeroValue = "0000000000000000000000000000000000000000"

// SetRef returns a RefMutation that unconditionally sets a ref to the given
// value. The ref does not need to have previously existed.
func SetRef(newvalue string) RefMutation {
	if newvalue == refZeroValue {
		return RefMutation{command: "updateerror"}
	}
	return RefMutation{command: "update", newvalue: newvalue}
}

// SetRefIfMatches returns a RefMutation that sets a ref to newvalue, failing
// if the ref does not have the given oldvalue.
func SetRefIfMatches(oldvalue, newvalue string) RefMutation {
	if newvalue == refZeroValue || oldvalue == refZeroValue {
		return RefMutation{command: "updateerror"}
	}
	return RefMutation{command: "update", newvalue: newvalue}
}

// CreateRef returns a RefMutation that creates a ref with the given value,
// failing if the ref already exists.
func CreateRef(newvalue string) RefMutation {
	if newvalue == refZeroValue {
		return RefMutation{command: "createerror"}
	}
	return RefMutation{command: "create", newvalue: newvalue}
}

// DeleteRef returns a RefMutation that unconditionally deletes a ref.
func DeleteRef() RefMutation {
	return RefMutation{command: "delete"}
}

// DeleteRefIfMatches returns a RefMutation that attempts to delete a ref, but
// fails if it has the given value.
func DeleteRefIfMatches(oldvalue string) RefMutation {
	if oldvalue == refZeroValue {
		return RefMutation{command: "deleteerror"}
	}
	return RefMutation{command: "delete", oldvalue: oldvalue}
}

// IsNoop reports whether mut is a no-op.
func (mut RefMutation) IsNoop() bool {
	return mut.command == ""
}

func (mut RefMutation) error() string {
	const suffix = "error"
	if !strings.HasSuffix(mut.command, suffix) {
		return ""
	}
	return "invalid " + mut.command[:len(mut.command)-len(suffix)]
}

// String returns the mutation in a form similar to a line of input to
// `git update-ref --stdin`.
func (mut RefMutation) String() string {
	if err := mut.error(); err != "" {
		return "<" + err + ">"
	}
	switch mut.command {
	case "":
		return ""
	case "update", "create":
		if mut.oldvalue != "" {
			return mut.command + " <ref> " + mut.newvalue + " " + mut.oldvalue
		}
		return mut.command + " <ref> " + mut.newvalue
	case "delete", "verify":
		if mut.oldvalue != "" {
			return mut.command + " <ref> " + mut.oldvalue
		}
		return mut.command + " <ref>"
	default:
		return mut.command + " <ref>"
	}
}

// MutateRefs atomically modifies zero or more refs. If there are no non-zero
// mutations, then MutateRefs returns nil without running Git.
func (g *Git) MutateRefs(ctx context.Context, muts map[Ref]RefMutation) error {
	input := new(bytes.Buffer)
	for ref, mut := range muts {
		if mut.IsNoop() {
			continue
		}
		if err := mut.error(); err != "" {
			return fmt.Errorf("git update-ref: %v: %s", ref, err)
		}
		input.WriteString(mut.command)
		input.WriteByte(' ')
		input.WriteString(ref.String())
		input.WriteByte(0)
		switch mut.command {
		case "update":
			input.WriteString(mut.newvalue)
			input.WriteByte(0)
			input.WriteString(mut.oldvalue)
			input.WriteByte(0)
		case "create":
			input.WriteString(mut.newvalue)
			input.WriteByte(0)
		case "delete", "verify":
			input.WriteString(mut.oldvalue)
			input.WriteByte(0)
		default:
			panic("unknown command " + mut.command)
		}
	}
	if input.Len() == 0 {
		return nil
	}

	output := new(bytes.Buffer)
	w := &limitWriter{w: output, n: errorOutputLimit}
	err := g.runner.RunGit(ctx, &Invocation{
		Args:   []string{"update-ref", "--stdin", "-z"},
		Dir:    g.dir,
		Stdin:  input,
		Stdout: w,
		Stderr: w,
	})
	if err != nil {
		return commandError("git update-ref", err, output.Bytes())
	}
	return nil
}

// Rev is a parsed reference to a single commit.
type Rev struct {
	Commit Hash
	Ref    Ref
}

// String returns the shortest symbolic name if possible, falling back
// to the commit hash.
func (r *Rev) String() string {
	if b := r.Ref.Branch(); b != "" {
		return b
	}
	if r.Ref.IsValid() {
		return r.Ref.String()
	}
	return r.Commit.String()
}
