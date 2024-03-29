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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	slashpath "path"
	"strconv"
	"strings"
	"time"

	"gg-scm.io/pkg/git/object"
)

// WorkTree determines the absolute path of the root of the current
// working tree given the configuration. Any symlinks are resolved.
func (g *Git) WorkTree(ctx context.Context) (string, error) {
	const errPrefix = "find git work tree root"
	out, err := g.output(ctx, errPrefix, []string{"rev-parse", "--show-toplevel"})
	if err != nil {
		return "", err
	}
	line, err := oneLine(out)
	if err != nil {
		return "", fmt.Errorf("%s: %w", errPrefix, err)
	}
	evaled, err := g.fs.EvalSymlinks(line)
	if err != nil {
		return "", fmt.Errorf("%s: %w", errPrefix, err)
	}
	return evaled, nil
}

// prefix returns the path of the working directory relative to the root
// of the working tree.
func (g *Git) prefix(ctx context.Context) (string, error) {
	const errPrefix = "prefix"
	prefix, err := g.output(ctx, errPrefix, []string{"rev-parse", "--show-prefix"})
	if err != nil {
		return "", err
	}
	prefix, err = oneLine(prefix)
	if err != nil {
		return "", fmt.Errorf("%s: %w", errPrefix, err)
	}
	return prefix, nil
}

// GitDir determines the absolute path of the Git directory for this
// working tree given the configuration. Any symlinks are resolved.
func (g *Git) GitDir(ctx context.Context) (string, error) {
	const errPrefix = "find .git directory"
	out, err := g.output(ctx, errPrefix, []string{"rev-parse", "--absolute-git-dir"})
	if err != nil {
		return "", err
	}
	line, err := oneLine(out)
	if err != nil {
		return "", fmt.Errorf("%s: %w", errPrefix, err)
	}
	evaled, err := g.fs.EvalSymlinks(g.fs.Clean(line))
	if err != nil {
		return "", fmt.Errorf("%s: %w", errPrefix, err)
	}
	return evaled, nil
}

// CommonDir determines the absolute path of the Git directory, possibly
// shared among different working trees, given the configuration. Any
// symlinks are resolved.
func (g *Git) CommonDir(ctx context.Context) (string, error) {
	const errPrefix = "find .git directory"
	out, err := g.output(ctx, errPrefix, []string{"rev-parse", "--git-common-dir"})
	if err != nil {
		return "", err
	}
	line, err := oneLine(out)
	if err != nil {
		return "", fmt.Errorf("%s: %w", errPrefix, err)
	}
	commonDir := g.abs(line)
	if err != nil {
		return "", fmt.Errorf("%s: %w", errPrefix, err)
	}
	evaled, err := g.fs.EvalSymlinks(commonDir)
	if err != nil {
		return "", fmt.Errorf("%s: %w", errPrefix, err)
	}
	return evaled, nil
}

// IsMerging reports whether the index has a pending merge commit.
func (g *Git) IsMerging(ctx context.Context) (bool, error) {
	const errPrefix = "check git merge"
	gitDir, err := g.GitDir(ctx)
	if err != nil {
		return false, fmt.Errorf("%s: %w", errPrefix, err)
	}
	_, err = os.Stat(g.fs.Join(gitDir, "MERGE_HEAD"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%s: %w", errPrefix, err)
	}
	return true, nil
}

// MergeBase returns the best common ancestor between two commits to use
// in a three-way merge.
func (g *Git) MergeBase(ctx context.Context, rev1, rev2 string) (Hash, error) {
	errPrefix := fmt.Sprintf("git merge-base %q %q", rev1, rev2)
	if err := validateRev(rev1); err != nil {
		return Hash{}, fmt.Errorf("%s: %w", errPrefix, err)
	}
	if err := validateRev(rev2); err != nil {
		return Hash{}, fmt.Errorf("%s: %w", errPrefix, err)
	}
	out, err := g.output(ctx, errPrefix, []string{"merge-base", rev1, rev2})
	if err != nil {
		return Hash{}, err
	}
	h, err := ParseHash(strings.TrimSuffix(out, "\n"))
	if err != nil {
		return Hash{}, fmt.Errorf("%s: %w", errPrefix, err)
	}
	return h, nil
}

// IsAncestor reports whether rev1 is an ancestor of rev2.
// If rev1 == rev2, then IsAncestor returns true.
func (g *Git) IsAncestor(ctx context.Context, rev1, rev2 string) (bool, error) {
	errPrefix := fmt.Sprintf("git: check %q ancestor of %q", rev1, rev2)
	if err := validateRev(rev1); err != nil {
		return false, fmt.Errorf("%s: %w", errPrefix, err)
	}
	if err := validateRev(rev2); err != nil {
		return false, fmt.Errorf("%s: %w", errPrefix, err)
	}
	err := g.run(ctx, errPrefix, []string{"merge-base", "--is-ancestor", rev1, rev2})
	if err != nil {
		if exitCode(err) == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// TreeEntry represents a single entry in a Git tree object.
// It implements [fs.FileInfo].
type TreeEntry struct {
	size int64
	raw  object.TreeEntry
}

// Name returns the base name of the file.
func (ent *TreeEntry) Name() string { return slashpath.Base(string(ent.raw.Name)) }

// Path returns the file's path relative to the root of the repository.
func (ent *TreeEntry) Path() TopPath { return TopPath(ent.raw.Name) }

// Size returns the length in bytes for blobs.
func (ent *TreeEntry) Size() int64 { return ent.size }

// Mode returns the file mode bits.
func (ent *TreeEntry) Mode() fs.FileMode {
	mode, ok := ent.raw.Mode.FileMode()
	if !ok {
		panic("unsupported mode")
	}
	return mode
}

// ModTime returns the zero time. It exists purely to satisfy the
// [fs.FileInfo] interface.
func (ent *TreeEntry) ModTime() time.Time { return time.Time{} }

// IsDir reports whether the file mode indicates a directory.
func (ent *TreeEntry) IsDir() bool { return ent.raw.Mode.IsDir() }

// Sys returns nil. It exists purely to satisfy the [fs.FileInfo] interface.
func (ent *TreeEntry) Sys() interface{} { return nil }

// ObjectType returns the file's Git object type.
func (ent *TreeEntry) ObjectType() object.Type {
	switch ent.raw.Mode {
	case object.ModeGitlink:
		return object.TypeCommit
	case object.ModeDir:
		return object.TypeTree
	default:
		return object.TypeBlob
	}
}

// Object returns the hash of the file's Git object.
func (ent *TreeEntry) Object() Hash { return ent.raw.ObjectID }

// String formats the entry similar to `git ls-tree` output.
func (ent *TreeEntry) String() string {
	return fmt.Sprintf("%v %s %v %s", ent.raw.Mode, ent.ObjectType(), ent.raw.ObjectID, ent.raw.Name)
}

// ListTreeOptions specifies the command-line options for `git ls-tree`.
type ListTreeOptions struct {
	// If Pathspecs is not empty, then it is used to filter the paths.
	Pathspecs []Pathspec
	// If Recursive is true, then the command will recurse into sub-trees.
	Recursive bool
	// If NameOnly is true, then only the keys of the map returned from ListTree
	// will be populated (the values will be nil). This can be more efficient if
	// information beyond the name is not needed.
	NameOnly bool
}

// ListTree returns the list of files at a given revision.
func (g *Git) ListTree(ctx context.Context, rev string, opts ListTreeOptions) (map[TopPath]*TreeEntry, error) {
	errPrefix := fmt.Sprintf("git ls-tree %q", rev)
	if err := validateRev(rev); err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	args := []string{"ls-tree", "-z"}
	if opts.Recursive {
		args = append(args, "-r")
	}
	if opts.NameOnly {
		args = append(args, "--name-only")
	} else {
		// Include object size.
		args = append(args, "--long")
	}
	if len(opts.Pathspecs) == 0 {
		args = append(args, "--full-tree", rev)
	} else {
		// Use --full-name, as --full-tree interprets the path arguments
		// relative to the top of the directory.
		args = append(args, "--full-name", rev, "--")
		for _, p := range opts.Pathspecs {
			args = append(args, p.String())
		}
	}
	out, err := g.output(ctx, errPrefix, args)
	if err != nil {
		return nil, err
	}
	tree := make(map[TopPath]*TreeEntry)
	if opts.NameOnly {
		for len(out) > 0 {
			i := strings.IndexByte(out, 0)
			if i == -1 {
				return tree, fmt.Errorf("%s: %w", errPrefix, io.ErrUnexpectedEOF)
			}
			tree[TopPath(out[:i])] = nil
			out = out[i+1:]
		}
	} else {
		for len(out) > 0 {
			ent, trail, err := parseTreeEntry(out)
			if err != nil {
				return tree, fmt.Errorf("%s: %w", errPrefix, err)
			}
			tree[TopPath(ent.raw.Name)] = ent
			out = trail
		}
	}
	return tree, nil
}

func parseTreeEntry(out string) (_ *TreeEntry, trail string, _ error) {
	end := strings.IndexByte(out, 0)
	if end == -1 {
		return nil, "", io.ErrUnexpectedEOF
	}
	trail = out[end+1:]
	entryEnd := strings.IndexByte(out[:end], '\t')
	if entryEnd == -1 {
		return nil, trail, errors.New("missing \\t in entry")
	}
	ent := &TreeEntry{raw: object.TreeEntry{Name: string(out[entryEnd+1 : end])}}
	parts := strings.SplitN(out[:entryEnd], " ", 4)
	if len(parts) != 4 {
		return nil, trail, fmt.Errorf("%s: entry has %d fields (4 expected)", ent.raw.Name, len(parts))
	}

	mode, err := strconv.ParseUint(parts[0], 8, 32)
	if err != nil {
		return nil, trail, fmt.Errorf("%s: mode: %v", ent.raw.Name, err)
	}
	ent.raw.Mode = object.Mode(mode)
	if _, ok := ent.raw.Mode.FileMode(); !ok {
		return nil, trail, fmt.Errorf("%s: mode: %v unsupported", ent.raw.Name, ent.raw.Mode)
	}

	if got, expect := object.Type(parts[1]), ent.ObjectType(); got != expect {
		return nil, trail, fmt.Errorf("%s: object: type is %q (expected %q based on mode %v)", ent.raw.Name, got, expect, ent.raw.Mode)
	}
	ent.raw.ObjectID, err = ParseHash(parts[2])
	if err != nil {
		return nil, trail, fmt.Errorf("%s: object: %v", ent.raw.Name, err)
	}
	if size := strings.TrimLeft(parts[3], " "); size != "-" {
		ent.size, err = strconv.ParseInt(size, 10, 64)
		if err != nil {
			return nil, trail, fmt.Errorf("%s: object size: %v", ent.raw.Name, err)
		}
	}
	return ent, trail, nil
}

// Cat reads the content of a file at a particular revision.
// It is the caller's responsibility to close the returned io.ReadCloser
// if the returned error is nil.
func (g *Git) Cat(ctx context.Context, rev string, path TopPath) (io.ReadCloser, error) {
	errPrefix := fmt.Sprintf("git cat %q @ %q", path, rev)
	if err := validateRev(rev); err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	if strings.Contains(rev, ":") {
		return nil, fmt.Errorf("%s: revision contains ':'", errPrefix)
	}
	if path == "" {
		return nil, fmt.Errorf("%s: empty path", errPrefix)
	}
	if strings.HasPrefix(string(path), "./") || strings.HasPrefix(string(path), "../") {
		return nil, fmt.Errorf("%s: path is relative", errPrefix)
	}
	stderr := new(bytes.Buffer)
	stdout, err := StartPipe(ctx, g.runner, &Invocation{
		Args:   []string{"cat-file", string(object.TypeBlob), rev + ":" + path.String()},
		Dir:    g.dir,
		Stderr: &limitWriter{w: stderr, n: errorOutputLimit},
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}

	// If Git reports an error, stdout will be empty and stderr will
	// contain the error message.
	first := make([]byte, 2048)
	readLen, readErr := io.ReadAtLeast(stdout, first, 1)
	if readErr != nil {
		// Empty stdout, check for error.
		if err := stdout.Close(); err != nil {
			return nil, commandError(errPrefix, err, stderr.Bytes())
		}
		if !errors.Is(readErr, io.EOF) {
			return nil, commandError(errPrefix, readErr, stderr.Bytes())
		}
		return nopReader{}, nil
	}
	return &catReader{
		errPrefix: errPrefix,
		first:     first[:readLen],
		pipe:      stdout,
		stderr:    stderr,
	}, nil
}

type catReader struct {
	errPrefix string
	first     []byte
	pipe      io.ReadCloser
	stderr    *bytes.Buffer // can't be read until wait returns
}

func (cr *catReader) Read(p []byte) (int, error) {
	if len(cr.first) > 0 {
		n := copy(p, cr.first)
		cr.first = cr.first[n:]
		return n, nil
	}
	return cr.pipe.Read(p)
}

func (cr *catReader) Close() error {
	err := cr.pipe.Close()
	if err != nil {
		return commandError("close "+cr.errPrefix, err, cr.stderr.Bytes())
	}
	return nil
}

// Init ensures a repository exists at the given path. Any relative paths are
// interpreted relative to the Git process's working directory. If any of the
// repository's parent directories don't exist, they will be created.
func (g *Git) Init(ctx context.Context, dir string) error {
	errPrefix := fmt.Sprintf("git init %q", dir)
	_, err := g.fs.EvalSymlinks(g.fs.Join(g.abs(dir), ".git"))
	dirExists := err == nil

	err = g.run(ctx, errPrefix, []string{"init", "--quiet", "--", dir})
	if err != nil {
		return err
	}

	if !dirExists {
		if err := g.WithDir(dir).linkToMain(ctx); err != nil {
			return fmt.Errorf("%s: %w", errPrefix, err)
		}
	}
	return nil
}

// InitBare ensures a bare repository exists at the given path. Any relative
// paths are interpreted relative to the Git process's working directory. If any
// of the repository's parent directories don't exist, they will be created.
func (g *Git) InitBare(ctx context.Context, dir string) error {
	errPrefix := fmt.Sprintf("git init %q", dir)
	_, err := g.fs.EvalSymlinks(g.fs.Join(g.abs(dir), "HEAD"))
	headExists := err == nil

	err = g.run(ctx, errPrefix, []string{"init", "--quiet", "--bare", "--", dir})
	if err != nil {
		return err
	}

	if !headExists {
		if err := g.WithDir(dir).linkToMain(ctx); err != nil {
			return fmt.Errorf("%s: %w", errPrefix, err)
		}
	}
	return nil
}

func (g *Git) linkToMain(ctx context.Context) error {
	const errPrefix = "git symbolic-ref HEAD refs/heads/main"
	return g.run(ctx, errPrefix, []string{"symbolic-ref", "HEAD", "refs/heads/main"})
}

// AddOptions specifies the command-line options for `git add`.
type AddOptions struct {
	// IncludeIgnored specifies whether to add ignored files.
	// If this is false and an ignored file is explicitly named, then Add
	// will return an error while other matched files are still added.
	IncludeIgnored bool
	// If IntentToAdd is true, then contents of files in the index will
	// not be changed, but any untracked files will have entries added
	// into the index with empty content.
	IntentToAdd bool
}

// Add adds file contents to the index. If len(pathspecs) == 0, then Add returns nil.
func (g *Git) Add(ctx context.Context, pathspecs []Pathspec, opts AddOptions) error {
	if len(pathspecs) == 0 {
		return nil
	}
	args := []string{"add"}
	if opts.IncludeIgnored {
		args = append(args, "-f")
	}
	if opts.IntentToAdd {
		args = append(args, "-N")
	}
	args = append(args, "--")
	for _, p := range pathspecs {
		args = append(args, p.String())
	}
	return g.run(ctx, "git add", args)
}

// StageTracked updates the index to match the tracked files in the
// working copy.
func (g *Git) StageTracked(ctx context.Context) error {
	return g.run(ctx, "git add -u", []string{"add", "--update"})
}

// RemoveOptions specifies the command-line options for `git add`.
type RemoveOptions struct {
	// Recursive specifies whether to remove directories.
	Recursive bool
	// If Modified is true, then files will be deleted even if they've
	// been modified from their checked in state.
	Modified bool
	// If KeepWorkingCopy is true, then the file will only be removed in
	// the index, not the working copy.
	KeepWorkingCopy bool
}

// Remove removes file contents from the index.
func (g *Git) Remove(ctx context.Context, pathspecs []Pathspec, opts RemoveOptions) error {
	if len(pathspecs) == 0 {
		return nil
	}
	args := []string{"rm", "--quiet"}
	if opts.Recursive {
		args = append(args, "-r")
	}
	if opts.Modified {
		args = append(args, "--force")
	}
	if opts.KeepWorkingCopy {
		args = append(args, "--cached")
	}
	args = append(args, "--")
	for _, p := range pathspecs {
		args = append(args, p.String())
	}
	return g.run(ctx, "git rm", args)
}

// CommitOptions overrides the default metadata for a commit. Any fields
// with zero values will use the value inferred from Git's environment.
type CommitOptions struct {
	Author     object.User
	AuthorTime time.Time
	Committer  object.User
	CommitTime time.Time

	// If SkipHooks is true, pre-commit and commit-msg hooks will be skipped.
	SkipHooks bool
}

func (opts CommitOptions) addToEnv(env []string) []string {
	if opts.Author != "" {
		env = append(env, "GIT_AUTHOR_NAME="+opts.Author.Name())
		env = append(env, "GIT_AUTHOR_EMAIL="+opts.Author.Email())
	}
	if !opts.AuthorTime.IsZero() {
		env = append(env, "GIT_AUTHOR_DATE="+opts.AuthorTime.Format(time.RFC3339))
	}
	if opts.Committer != "" {
		env = append(env, "GIT_COMMITTER_NAME="+opts.Committer.Name())
		env = append(env, "GIT_COMMITTER_EMAIL="+opts.Committer.Email())
	}
	if !opts.CommitTime.IsZero() {
		env = append(env, "GIT_COMMITTER_DATE="+opts.CommitTime.Format(time.RFC3339))
	}
	return env
}

func (opts CommitOptions) addToArgs(args []string) []string {
	if opts.SkipHooks {
		args = append(args, "--no-verify")
	}
	return args
}

// Commit creates a new commit on HEAD with the staged content.
// The message will be used exactly as given.
func (g *Git) Commit(ctx context.Context, message string, opts CommitOptions) error {
	out := new(bytes.Buffer)
	w := &limitWriter{w: out, n: errorOutputLimit}
	err := g.runner.RunGit(ctx, &Invocation{
		Args:   opts.addToArgs([]string{"commit", "--quiet", "--file=-", "--cleanup=verbatim"}),
		Dir:    g.dir,
		Env:    opts.addToEnv(nil),
		Stdin:  strings.NewReader(message),
		Stdout: w,
		Stderr: w,
	})
	if err != nil {
		return commandError("git commit", err, out.Bytes())
	}
	return nil
}

// CommitAll creates a new commit on HEAD with all of the tracked files.
// The message will be used exactly as given.
func (g *Git) CommitAll(ctx context.Context, message string, opts CommitOptions) error {
	out := new(bytes.Buffer)
	w := &limitWriter{w: out, n: errorOutputLimit}
	err := g.runner.RunGit(ctx, &Invocation{
		Args:   opts.addToArgs([]string{"commit", "--quiet", "--file=-", "--cleanup=verbatim", "--all"}),
		Dir:    g.dir,
		Env:    opts.addToEnv(nil),
		Stdin:  strings.NewReader(message),
		Stdout: w,
		Stderr: w,
	})
	if err != nil {
		return commandError("git commit", err, out.Bytes())
	}
	return nil
}

// CommitFiles creates a new commit on HEAD that updates the given files
// to the content in the working copy. The message will be used exactly
// as given.
func (g *Git) CommitFiles(ctx context.Context, message string, pathspecs []Pathspec, opts CommitOptions) error {
	const errPrefix = "git commit"
	if len(pathspecs) > 0 {
		prefix, err := g.prefix(ctx)
		if err != nil {
			return fmt.Errorf("%s: %w", errPrefix, err)
		}
		if prefix != "" {
			// Always run from top of worktree to avoid Git bug detailed in
			// https://github.com/zombiezen/gg/issues/10
			workTree, err := g.WorkTree(ctx)
			if err != nil {
				return fmt.Errorf("%s: %w", errPrefix, err)
			}
			g = g.WithDir(workTree)

			// Rewrite pathspecs as needed.
			pathspecs = append([]Pathspec(nil), pathspecs...)
			for i := range pathspecs {
				magic, pat := pathspecs[i].SplitMagic()
				if magic.Top || g.fs.IsAbs(pat) {
					// Top-level or absolute paths need no rewrite.
					continue
				}
				pathspecs[i] = JoinPathspecMagic(magic, g.fs.Join(prefix, pat))
			}
		}
	}
	args := []string{"commit", "--quiet", "--file=-", "--cleanup=verbatim", "--only", "--allow-empty"}
	args = opts.addToArgs(args)
	args = append(args, "--")
	for _, spec := range pathspecs {
		args = append(args, spec.String())
	}
	out := new(bytes.Buffer)
	w := &limitWriter{w: out, n: errorOutputLimit}
	err := g.runner.RunGit(ctx, &Invocation{
		Args:   args,
		Dir:    g.dir,
		Env:    opts.addToEnv(nil),
		Stdin:  strings.NewReader(message),
		Stdout: w,
		Stderr: w,
	})
	if err != nil {
		return commandError("git commit", err, out.Bytes())
	}
	return nil
}

// AmendOptions overrides the previous commit's fields.
type AmendOptions struct {
	// If Message is not empty, it is the commit message that will be used.
	// Otherwise, the previous commit's message will be used.
	Message string
	// If Author is filled out, then it will be used as the commit author.
	// If Author is blank, then the previous commit's author will be used.
	Author object.User
	// If AuthorTime is not zero, then it will be used as the author time.
	// Otherwise, the previous commit's author time will be used.
	AuthorTime time.Time

	// If Committer is not empty, then it will override the default committer
	// information from Git configuration.
	Committer object.User
	// If CommitTime is not zero, then it will be used as the commit time
	// instead of now.
	CommitTime time.Time

	// If SkipHooks is true, pre-commit and commit-msg hooks will be skipped.
	SkipHooks bool
}

func (opts AmendOptions) addToArgs(args []string) []string {
	if opts.Author != "" {
		args = append(args, "--author="+string(opts.Author))
	}
	if !opts.AuthorTime.IsZero() {
		args = append(args, "--date="+opts.AuthorTime.Format(time.RFC3339))
	}
	if opts.SkipHooks {
		args = append(args, "--no-verify")
	}
	return args
}

func (opts AmendOptions) addToEnv(env []string) []string {
	if opts.Committer != "" {
		env = append(env, "GIT_COMMITTER_NAME="+opts.Committer.Name())
		env = append(env, "GIT_COMMITTER_EMAIL="+opts.Committer.Email())
	}
	if !opts.CommitTime.IsZero() {
		env = append(env, "GIT_COMMITTER_DATE="+opts.CommitTime.Format(time.RFC3339))
	}
	return env
}

// Amend replaces the tip of the current branch with a new commit with
// the content of the index.
func (g *Git) Amend(ctx context.Context, opts AmendOptions) error {
	const errPrefix = "git commit --amend"
	msg := opts.Message
	if msg == "" {
		info, err := g.CommitInfo(ctx, "HEAD")
		if err != nil {
			return fmt.Errorf("%s: %w", errPrefix, err)
		}
		msg = info.Message
	}
	out := new(bytes.Buffer)
	w := &limitWriter{w: out, n: errorOutputLimit}
	err := g.runner.RunGit(ctx, &Invocation{
		Args: opts.addToArgs([]string{
			"commit",
			"--amend",
			"--quiet",
			"--file=-",
			"--cleanup=verbatim",
		}),
		Dir:    g.dir,
		Env:    opts.addToEnv(nil),
		Stdin:  strings.NewReader(msg),
		Stdout: w,
		Stderr: w,
	})
	if err != nil {
		return commandError(errPrefix, err, out.Bytes())
	}
	return nil
}

// AmendAll replaces the tip of the current branch with a new commit
// with the content of the working copy for all tracked files.
func (g *Git) AmendAll(ctx context.Context, opts AmendOptions) error {
	const errPrefix = "git commit --amend --all"
	msg := opts.Message
	if msg == "" {
		info, err := g.CommitInfo(ctx, "HEAD")
		if err != nil {
			return fmt.Errorf("%s: %w", errPrefix, err)
		}
		msg = info.Message
	}
	out := new(bytes.Buffer)
	w := &limitWriter{w: out, n: errorOutputLimit}
	err := g.runner.RunGit(ctx, &Invocation{
		Args: opts.addToArgs([]string{
			"commit",
			"--amend",
			"--all",
			"--quiet",
			"--file=-",
			"--cleanup=verbatim",
		}),
		Dir:    g.dir,
		Env:    opts.addToEnv(nil),
		Stdin:  strings.NewReader(msg),
		Stdout: w,
		Stderr: w,
	})
	if err != nil {
		return commandError(errPrefix, err, out.Bytes())
	}
	return nil
}

// AmendFiles replaces the tip of the current branch with a new commit
// with the content of the named files from the working copy. Files not
// named will get their content from the previous commit.
//
// Notably, AmendFiles with no paths will not change the file content of
// the commit, just the options specified.
func (g *Git) AmendFiles(ctx context.Context, pathspecs []Pathspec, opts AmendOptions) error {
	const errPrefix = "git commit --amend --only"
	msg := opts.Message
	if msg == "" {
		info, err := g.CommitInfo(ctx, "HEAD")
		if err != nil {
			return fmt.Errorf("%s: %w", errPrefix, err)
		}
		msg = info.Message
	}
	if len(pathspecs) > 0 {
		prefix, err := g.prefix(ctx)
		if err != nil {
			return fmt.Errorf("%s: %w", errPrefix, err)
		}
		if prefix != "" {
			// Always run from top of worktree to avoid Git bug detailed in
			// https://github.com/zombiezen/gg/issues/10
			workTree, err := g.WorkTree(ctx)
			if err != nil {
				return fmt.Errorf("%s: %w", errPrefix, err)
			}
			g = g.WithDir(workTree)

			// Rewrite pathspecs as needed.
			pathspecs = append([]Pathspec(nil), pathspecs...)
			for i := range pathspecs {
				magic, pat := pathspecs[i].SplitMagic()
				if magic.Top || g.fs.IsAbs(pat) {
					// Top-level or absolute paths need no rewrite.
					continue
				}
				pathspecs[i] = JoinPathspecMagic(magic, g.fs.Join(prefix, pat))
			}
		}
	}
	args := opts.addToArgs([]string{
		"commit",
		"--amend",
		"--only",
		"--quiet",
		"--file=-",
		"--cleanup=verbatim",
	})
	if len(pathspecs) > 0 {
		args = append(args, "--")
		for _, p := range pathspecs {
			args = append(args, p.String())
		}
	}
	out := new(bytes.Buffer)
	w := &limitWriter{w: out, n: errorOutputLimit}
	err := g.runner.RunGit(ctx, &Invocation{
		Args:   args,
		Dir:    g.dir,
		Env:    opts.addToEnv(nil),
		Stdin:  strings.NewReader(msg),
		Stdout: w,
		Stderr: w,
	})
	if err != nil {
		return commandError(errPrefix, err, out.Bytes())
	}
	return nil
}

// Merge merges changes from the named revisions into the index and the
// working copy. It updates MERGE_HEAD but does not create a commit.
// Merge will never perform a fast-forward merge.
//
// In case of conflict, Merge will return an error but still update
// MERGE_HEAD. To check for this condition, call IsMerging after
// receiving an error from Merge (verifying that IsMerging returned
// false before calling Merge).
func (g *Git) Merge(ctx context.Context, revs []string) error {
	errPrefix := "git merge"
	if len(revs) == 0 {
		return errors.New(errPrefix + ": no revisions")
	}
	for _, rev := range revs {
		if err := validateRev(rev); err != nil {
			return fmt.Errorf("%s: %w", errPrefix, err)
		}
	}
	if len(revs) == 1 {
		errPrefix += " " + revs[0]
	}
	args := []string{"merge", "--quiet", "--no-commit", "--no-ff"}
	args = append(args, revs...)
	return g.run(ctx, errPrefix, args)
}

// AbortMerge aborts the current conflict resolution process and tries
// to reconstruct pre-merge state.
func (g *Git) AbortMerge(ctx context.Context) error {
	return g.run(ctx, "git abort merge", []string{"merge", "--abort"})
}

// CheckoutOptions specifies the command-line options for `git checkout`.
type CheckoutOptions struct {
	// ConflictBehavior specifies the behavior when encountering locally
	// modified files.
	ConflictBehavior CheckoutConflictBehavior
}

// CheckoutConflictBehavior specifies the behavior of checkout with
// local modifications.
type CheckoutConflictBehavior int

// Possible checkout behaviors when encountering locally modified files.
const (
	// AbortOnFileChange stops the checkout if a file that is modified
	// locally differs between the current HEAD and the target commit.
	// This is the default behavior.
	AbortOnFileChange CheckoutConflictBehavior = iota
	// MergeLocal performs a three-way merge on any differing files.
	MergeLocal
	// DiscardLocal uses the target commit's content regardless of local
	// modifications.
	DiscardLocal
)

// String returns the Go constant name of the behavior.
func (ccb CheckoutConflictBehavior) String() string {
	switch ccb {
	case AbortOnFileChange:
		return "AbortOnFileChange"
	case MergeLocal:
		return "MergeLocal"
	case DiscardLocal:
		return "DiscardLocal"
	default:
		return fmt.Sprintf("CheckoutConflictBehavior(%d)", int(ccb))
	}
}

// CheckoutBranch switches HEAD to another branch and updates the
// working copy to match. If the branch does not exist, then
// CheckoutBranch returns an error.
func (g *Git) CheckoutBranch(ctx context.Context, branch string, opts CheckoutOptions) error {
	errPrefix := fmt.Sprintf("git checkout %q", branch)
	if err := validateBranch(branch); err != nil {
		return fmt.Errorf("%s: %w", errPrefix, err)
	}
	if opts.ConflictBehavior != AbortOnFileChange && opts.ConflictBehavior != MergeLocal && opts.ConflictBehavior != DiscardLocal {
		return fmt.Errorf("%s: unknown conflict behavior in options", errPrefix)
	}
	// Verify that the branch exists. `git checkout` will attempt to
	// create branches if they don't exist if there's a remote tracking
	// branch of the same name.
	if err := g.run(ctx, errPrefix, []string{"rev-parse", "-q", "--verify", "--revs-only", BranchRef(branch).String()}); err != nil {
		return err
	}

	// Run checkout with branch name.
	args := []string{"checkout", "--quiet"}
	switch opts.ConflictBehavior {
	case MergeLocal:
		args = append(args, "--merge")
	case DiscardLocal:
		args = append(args, "--force")
	}
	args = append(args, branch, "--")
	if err := g.run(ctx, errPrefix, args); err != nil {
		return err
	}
	return nil
}

// CheckoutRev switches HEAD to a specific commit and updates the
// working copy to match. It will always put the worktree in "detached
// HEAD" state.
func (g *Git) CheckoutRev(ctx context.Context, rev string, opts CheckoutOptions) error {
	errPrefix := fmt.Sprintf("git checkout --detach %q", rev)
	if err := validateRev(rev); err != nil {
		return fmt.Errorf("%s: %w", errPrefix, err)
	}

	// Run checkout with the revision.
	args := []string{"checkout", "--quiet", "--detach"}
	switch opts.ConflictBehavior {
	case MergeLocal:
		args = append(args, "--merge")
	case DiscardLocal:
		args = append(args, "--force")
	}
	args = append(args, rev, "--")
	if err := g.run(ctx, errPrefix, args); err != nil {
		return err
	}
	return nil
}

// BranchOptions specifies options for a new branch.
type BranchOptions struct {
	// StartPoint is a revision to start from. If empty, then HEAD is used.
	StartPoint string
	// If Checkout is true, then HEAD and the working copy will be
	// switched to the new branch.
	Checkout bool
	// If Overwrite is true and a branch with the given name already
	// exists, then it will be reset to the start point. No other branch
	// information is modified, like the upstream.
	Overwrite bool
	// If Track is true and StartPoint names a ref, then the upstream of
	// the branch will be set to the ref named by StartPoint.
	Track bool
}

// NewBranch creates a new branch, a ref of the form "refs/heads/NAME",
// where NAME is the name argument.
func (g *Git) NewBranch(ctx context.Context, name string, opts BranchOptions) error {
	errPrefix := fmt.Sprintf("git branch %q", name)
	if err := validateBranch(name); err != nil {
		return fmt.Errorf("%s: %w", errPrefix, err)
	}
	if opts.StartPoint != "" {
		if err := validateRev(opts.StartPoint); err != nil {
			return fmt.Errorf("%s: %w", errPrefix, err)
		}
	}
	var args []string
	if opts.Checkout {
		args = append(args, "checkout", "--quiet")
		if opts.Track {
			args = append(args, "--track")
		} else {
			args = append(args, "--no-track")
		}
		if opts.Overwrite {
			args = append(args, "-B", name)
		} else {
			args = append(args, "-b", name)
		}
		if opts.StartPoint != "" {
			args = append(args, opts.StartPoint, "--")
		}
	} else {
		args = append(args, "branch", "--quiet")
		if opts.Track {
			args = append(args, "--track")
		} else {
			args = append(args, "--no-track")
		}
		if opts.Overwrite {
			args = append(args, "--force")
		}
		args = append(args, name)
		if opts.StartPoint != "" {
			args = append(args, opts.StartPoint)
		}
	}
	return g.run(ctx, errPrefix, args)
}

// DeleteBranchOptions specifies options for a new branch.
type DeleteBranchOptions struct {
	// If Force is true, then the branch will be deleted
	// even if the branch has not been merged
	// or if the branch does not point to a valid commit.
	Force bool
}

// DeleteBranches deletes zero or more branches.
// If names is empty, then DeleteBranches returns nil without running Git.
func (g *Git) DeleteBranches(ctx context.Context, names []string, opts DeleteBranchOptions) error {
	if len(names) == 0 {
		return nil
	}
	errPrefix := "git branch --delete " + strings.Join(names, " ")
	for _, name := range names {
		if err := validateBranch(name); err != nil {
			return fmt.Errorf("%s: %w", errPrefix, err)
		}
	}
	args := make([]string, 0, len(names)+4)
	args = append(args, "branch", "--delete")
	if opts.Force {
		args = append(args, "--force")
	}
	args = append(args, "--")
	args = append(args, names...)
	return g.run(ctx, errPrefix, args)
}

// NullTreeHash computes the hash of an empty tree and adds it to the
// repository. This is sometimes useful as a diff comparison target.
func (g *Git) NullTreeHash(ctx context.Context) (Hash, error) {
	const errPrefix = "git hash-object"
	out, err := g.output(ctx, errPrefix, []string{"hash-object", "-t", string(object.TypeTree), "--stdin"})
	if err != nil {
		return Hash{}, err
	}
	h, err := ParseHash(strings.TrimSuffix(out, "\n"))
	if err != nil {
		return Hash{}, fmt.Errorf("%s: %w", errPrefix, err)
	}
	return h, nil
}

// commandError returns a new error with the information from an
// unsuccessful run of a subprocess.
func commandError(prefix string, runError error, stderr []byte) error {
	stderr = bytes.TrimSuffix(stderr, []byte{'\n'})
	if len(stderr) == 0 {
		return fmt.Errorf("%s: %w", prefix, runError)
	}
	if exitCode(runError) != -1 {
		if bytes.IndexByte(stderr, '\n') == -1 {
			// Collapse into single line.
			return &formattedError{
				msg:   fmt.Sprintf("%s: %s", prefix, stderr),
				cause: runError,
			}
		}
		return &formattedError{
			msg:   fmt.Sprintf("%s:\n%s", prefix, stderr),
			cause: runError,
		}
	}
	return fmt.Errorf("%s: %w\n%s", prefix, runError, stderr)
}

type formattedError struct {
	msg   string
	cause error
}

func (e *formattedError) Error() string {
	return e.msg
}

func (e *formattedError) Unwrap() error {
	return e.cause
}

func validateRev(rev string) error {
	if rev == "" {
		return errors.New("empty revision")
	}
	if strings.HasPrefix(rev, "-") {
		return errors.New("revision cannot begin with dash")
	}
	return nil
}

func validateBranch(branch string) error {
	if branch == "" {
		return errors.New("empty branch")
	}
	if strings.HasPrefix(branch, "-") {
		return errors.New("branch cannot begin with dash")
	}
	return nil
}

type nopReader struct{}

func (nopReader) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

func (nopReader) Close() error {
	return nil
}
