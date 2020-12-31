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

	"gg-scm.io/pkg/git/object"
)

// CommitInfo obtains information about a single commit.
func (g *Git) CommitInfo(ctx context.Context, rev string) (*object.Commit, error) {
	errPrefix := fmt.Sprintf("git cat-file commit %q", rev)
	if err := validateRev(rev); err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	if strings.HasPrefix(rev, "^") {
		return nil, fmt.Errorf("%s: revision cannot be an exclusion", errPrefix)
	}
	if strings.Contains(rev, "..") {
		return nil, fmt.Errorf("%s: revision cannot be a range", errPrefix)
	}
	if strings.HasSuffix(rev, "^@") {
		return nil, fmt.Errorf("%s: revision cannot use parent shorthand", errPrefix)
	}

	out, err := g.output(ctx, errPrefix, []string{
		"cat-file",
		"commit",
		rev,
	})
	if err != nil {
		return nil, err
	}
	c, err := object.ParseCommit([]byte(out))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	return c, nil
}

// LogOptions specifies filters and ordering on a log listing.
type LogOptions struct {
	// Revs specifies the set of commits to list. When empty, it defaults
	// to all commits reachable from HEAD.
	Revs []string

	// MaxParents sets an inclusive upper limit on the number of parents
	// on revisions to return from Log. If MaxParents is zero, then it is
	// treated as no limit unless AllowZeroMaxParents is true.
	MaxParents          int
	AllowZeroMaxParents bool

	// If FirstParent is true, then follow only the first parent commit
	// upon seeing a merge commit.
	FirstParent bool

	// Limit specifies the upper bound on the number of revisions to return from Log.
	// Zero means no limit.
	Limit int

	// If Reverse is true, then commits will be returned in reverse order.
	Reverse bool

	// If NoWalk is true, then ancestor commits are not traversed. Does not have
	// an effect if Revs contains a range.
	NoWalk bool
}

// Log starts fetching information about a set of commits. The context's
// deadline and cancelation will apply to the entire read from the Log.
func (g *Git) Log(ctx context.Context, opts LogOptions) (*Log, error) {
	// TODO(someday): Add an example for this method.

	const errPrefix = "git rev-list"
	for _, rev := range opts.Revs {
		if err := validateRev(rev); err != nil {
			return nil, fmt.Errorf("%s: %w", errPrefix, err)
		}
	}
	args := []string{"rev-list", "--header"}
	if opts.MaxParents > 0 || opts.AllowZeroMaxParents {
		args = append(args, fmt.Sprintf("--max-parents=%d", opts.MaxParents))
	}
	if opts.FirstParent {
		args = append(args, "--first-parent")
	}
	if opts.Limit > 0 {
		args = append(args, fmt.Sprintf("--max-count=%d", opts.Limit))
	}
	if opts.Reverse {
		args = append(args, "--reverse")
	}
	if opts.NoWalk {
		args = append(args, "--no-walk=sorted")
	}
	if len(opts.Revs) == 0 {
		args = append(args, "HEAD")
	} else {
		args = append(args, opts.Revs...)
	}
	args = append(args, "--")

	ctx, cancel := context.WithCancel(ctx)
	stderr := new(bytes.Buffer)
	pipe, err := StartPipe(ctx, g.runner, &Invocation{
		Args:   args,
		Dir:    g.dir,
		Stderr: &limitWriter{w: stderr, n: 4096},
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}

	r := bufio.NewReaderSize(pipe, 1<<20 /* 1 MiB */)
	if _, err := r.Peek(1); err != nil && !errors.Is(err, io.EOF) {
		cancel()
		waitErr := pipe.Close()
		return nil, commandError(errPrefix, waitErr, stderr.Bytes())
	}
	return &Log{
		r:      r,
		cancel: cancel,
		close:  pipe,
	}, nil
}

// Log is an open handle to a `git rev-list` subprocess. Closing the Log
// stops the subprocess.
type Log struct {
	r      *bufio.Reader
	cancel context.CancelFunc
	close  io.Closer

	scanErr  error
	scanDone bool
	info     *object.Commit
}

// Next attempts to scan the next log entry and returns whether there is a new entry.
func (l *Log) Next() bool {
	if l.scanDone {
		return false
	}

	// Continue growing buffer until we've fit a log entry.
	end := -1
	for n := l.r.Buffered(); n < l.r.Size(); {
		data, err := l.r.Peek(n)
		end = bytes.IndexByte(data, 0)
		if end != -1 {
			break
		}
		if err != nil {
			switch {
			case err == io.EOF && l.r.Buffered() == 0:
				l.abort(nil)
			case err == io.EOF && l.r.Buffered() > 0:
				l.abort(io.ErrUnexpectedEOF)
			default:
				l.abort(err)
			}
			return false
		}
		if l.r.Buffered() > n {
			n = l.r.Buffered()
		} else {
			n++
		}
	}
	if end == -1 {
		l.abort(bufio.ErrBufferFull)
		return false
	}
	data, err := l.r.Peek(end)
	if err != nil {
		// Should already be buffered.
		panic(err)
	}

	// Read the commit hash (first line).
	firstLineEnd := bytes.IndexByte(data, '\n')
	if firstLineEnd == -1 {
		l.abort(fmt.Errorf("parse rev-list: missing line feed"))
		return false
	}
	var expectSum Hash
	if err := expectSum.UnmarshalText(data[:firstLineEnd]); err != nil {
		l.abort(err)
		return false
	}

	// Parse the commit.
	info, err := object.ParseCommit(data[firstLineEnd+1 : end])
	if err != nil {
		l.abort(fmt.Errorf("commit %v: %w", expectSum, err))
		return false
	}
	info.Message = cleanLogMessage(info.Message)
	if info.SHA1() != expectSum {
		l.abort(fmt.Errorf("commit %v: data does not match object ID", expectSum))
		return false
	}
	if _, err := l.r.Discard(end + 1); err != nil {
		// Should already be buffered.
		panic(err)
	}
	l.info = info
	return true
}

// cleanLogMessage removes the indents that `git rev-list` "helpfully" inserts
// into the message, despite the man page claiming "the raw format shows the
// entire commit exactly as stored in the commit object."
func cleanLogMessage(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, "    ")
	}
	return strings.Join(lines, "\n")
}

func (l *Log) abort(e error) {
	l.r = nil
	l.scanErr = e
	l.scanDone = true
	l.info = nil
	l.cancel()
}

// CommitInfo returns the most recently scanned log entry.
// Next must be called at least once before calling CommitInfo.
func (l *Log) CommitInfo() *object.Commit {
	return l.info
}

// Close ends the log subprocess and waits for it to finish.
// Close returns an error if Next returned false due to a parse failure.
func (l *Log) Close() error {
	// Not safe to call multiple times, but interface for Close doesn't
	// require this to be supported.

	l.cancel()
	l.close.Close() // Ignore error, since it's probably from interrupting.
	return l.scanErr
}
