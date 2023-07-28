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
	"crypto/sha1"
	"errors"
	"fmt"
	"hash"
	"io"
	"strconv"
	"strings"
	"sync"

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
		string(object.TypeCommit),
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

const logErrPrefix = "git rev-list | git cat-file --batch"

// Log starts fetching information about a set of commits. The context's
// deadline and cancelation will apply to the entire read from the Log.
func (g *Git) Log(ctx context.Context, opts LogOptions) (_ *Log, err error) {
	// TODO(someday): Add an example for this method.

	for _, rev := range opts.Revs {
		if err := validateRev(rev); err != nil {
			return nil, fmt.Errorf("%s: %w", logErrPrefix, err)
		}
	}
	args := []string{"rev-list"}
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
		args = append(args, Head.String())
	} else {
		args = append(args, opts.Revs...)
	}
	args = append(args, "--")

	ctx, cancel := context.WithCancel(ctx)
	stderr := new(bytes.Buffer)
	stderrMux := &muxWriter{w: &limitWriter{w: stderr, n: 4096}}

	revListStderr := stderrMux.newHandle()
	revListPipe, err := StartPipe(ctx, g.runner, &Invocation{
		Args:   args,
		Dir:    g.dir,
		Stderr: revListStderr,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("%s: %w", logErrPrefix, err)
	}

	catFileStderr := stderrMux.newHandle()
	catFilePipe, err := StartPipe(ctx, g.runner, &Invocation{
		Args:   []string{"cat-file", "--batch"},
		Dir:    g.dir,
		Stdin:  revListPipe,
		Stderr: catFileStderr,
	})
	if err != nil {
		cancel()
		revListPipe.Close()
		return nil, fmt.Errorf("%s: %w", logErrPrefix, err)
	}

	return &Log{
		r:          bufio.NewReaderSize(catFilePipe, 1<<20 /* 1 MiB */),
		stderr:     stderr,
		cancelFunc: cancel,
		hash:       sha1.New(),
		closers: [...]io.Closer{
			pipeStreamCloser{catFilePipe, catFileStderr},
			pipeStreamCloser{revListPipe, revListStderr},
		},
	}, nil
}

// Log is an open handle to a `git cat-file --batch` subprocess. Closing the Log
// stops the subprocess.
type Log struct {
	r          *bufio.Reader
	stderr     *bytes.Buffer
	hash       hash.Hash
	cancelFunc context.CancelFunc
	closers    [2]io.Closer

	scanErr  error
	scanDone bool
	info     *object.Commit
}

// Next attempts to scan the next log entry and returns whether there is a new entry.
func (l *Log) Next() bool {
	if l.scanDone {
		return false
	}
	err := l.next()
	if err != nil {
		l.cancel()
		l.scanErr = err
		if errors.Is(err, io.EOF) {
			l.scanErr = nil
		}
		return false
	}
	return true
}

func (l *Log) next() error {
	// Read object information.
	// Reference: https://git-scm.com/docs/git-cat-file#_batch_output
	line, err := l.r.ReadSlice('\n')
	if len(line) == 0 && errors.Is(err, io.EOF) {
		// Reached successful end. Wait for subprocesses to exit.
		if err := l.close(); err != nil {
			return err
		}
		return io.EOF
	}
	if err != nil {
		if errors.Is(err, io.EOF) {
			return io.ErrUnexpectedEOF
		}
		return err
	}
	fields := bytes.Fields(line)
	if len(fields) != 3 {
		return fmt.Errorf("invalid object information line")
	}
	var expectSum Hash
	if err := expectSum.UnmarshalText(fields[0]); err != nil {
		return err
	}
	if !bytes.Equal(fields[1], []byte(object.TypeCommit)) {
		return fmt.Errorf("commit %v: object is a %s", expectSum, fields[1])
	}
	size, err := strconv.Atoi(string(fields[2]))
	if err != nil {
		return fmt.Errorf("commit %v: size: %w", expectSum, err)
	}
	if size < 0 {
		return fmt.Errorf("commit %v: negative size", expectSum)
	}

	// Read commit object.
	data := make([]byte, size+1)
	if _, err := io.ReadFull(l.r, data); err != nil {
		return fmt.Errorf("commit %v: %w", expectSum, err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		return fmt.Errorf("commit %v: does not end with newline", expectSum)
	}
	// Validate commit object matches hash.
	data = data[:len(data)-1]
	l.hash.Reset()
	l.hash.Write(object.AppendPrefix(nil, object.TypeCommit, int64(size)))
	l.hash.Write(data)
	var gotSum Hash
	l.hash.Sum(gotSum[:0])
	if gotSum != expectSum {
		return fmt.Errorf("commit %v: data does not match object ID", expectSum)
	}
	// Parse commit.
	info, err := object.ParseCommit(data)
	if err != nil {
		return fmt.Errorf("commit %v: %w", expectSum, err)
	}
	l.info = info
	return nil
}

// CommitInfo returns the most recently scanned log entry.
// Next must be called at least once before calling CommitInfo.
func (l *Log) CommitInfo() *object.Commit {
	return l.info
}

// Close ends the log subprocess and waits for it to finish.
// Close returns an error if [Log.Next] returned false due to a parse failure.
// Subsequent calls to Close will no-op and return the same error.
func (l *Log) Close() error {
	l.cancel()
	l.close() // Ignore error, since it's from interrupting.
	return l.scanErr
}

func (l *Log) cancel() {
	l.cancelFunc()
	l.r = nil
	l.scanDone = true
	l.info = nil
}

func (l *Log) close() error {
	var first error
	for i, c := range l.closers {
		if c == nil {
			continue
		}
		if err := c.Close(); first == nil {
			first = err
		}
		l.closers[i] = nil
	}
	if first != nil {
		return commandError(logErrPrefix, first, l.stderr.Bytes())
	}
	return nil
}

// A muxWriter synchronizes access to a writer through a number of handles.
type muxWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (mux *muxWriter) newHandle() *muxWriterStream {
	return &muxWriterStream{
		buf: make([]byte, 0, 1024),
		mux: mux,
	}
}

// A muxWriterStream is a single stream of lines being sent to the muxWriter.
type muxWriterStream struct {
	buf []byte
	mux *muxWriter
}

func (stream *muxWriterStream) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	lastLF := bytes.LastIndexByte(data, '\n')
	if lastLF == -1 {
		// No newline; must buffer.
		newBufSize := len(stream.buf) + len(data)
		if newBufSize > cap(stream.buf) {
			return 0, errors.New("line too long")
		}
		stream.buf = append(stream.buf, data...)
		return len(data), nil
	}
	end := lastLF + 1

	stream.mux.mu.Lock()
	defer stream.mux.mu.Unlock()
	if err := stream.flushLocked(); err != nil {
		return 0, err
	}
	n, err := stream.mux.w.Write(data[:end])
	if err != nil {
		return n, err
	}
	trailing := data[end:]
	if len(trailing) > cap(stream.buf) {
		return n, errors.New("line too long")
	}
	stream.buf = append(stream.buf, trailing...)
	return len(data), nil
}

func (stream *muxWriterStream) Flush() error {
	stream.mux.mu.Lock()
	defer stream.mux.mu.Unlock()
	return stream.flushLocked()
}

func (stream *muxWriterStream) flushLocked() error {
	if len(stream.buf) == 0 {
		return nil
	}
	n, err := stream.mux.w.Write(stream.buf)
	if n < len(stream.buf) {
		// Move data to beginning to avoid dealing with buffer wraparound.
		newBufSize := copy(stream.buf, stream.buf[n:])
		stream.buf = stream.buf[:newBufSize]
	} else {
		stream.buf = stream.buf[:0]
	}
	return err
}

type pipeStreamCloser struct {
	pipe   io.Closer
	stream interface{ Flush() error }
}

func (c pipeStreamCloser) Close() error {
	err := c.pipe.Close()
	err2 := c.stream.Flush()
	if err == nil {
		err = err2
	}
	return err
}
