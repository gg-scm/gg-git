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

// Package git provides a high-level interface for interacting with
// a Git subprocess.
package git // import "gg-scm.io/pkg/git"

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"gg-scm.io/pkg/git/internal/sigterm"
)

// Git is a context for performing Git version control operations.
type Git struct {
	dir    string
	runner Runner
	fs     FileSystem

	versionMu   sync.Mutex
	versionCond chan struct{}
	version     string
}

// New creates a new Git context that communicates with a local Git subprocess.
// It is equivalent to passing the result of NewLocal to Custom.
func New(opts Options) (*Git, error) {
	l, err := NewLocal(opts)
	if err != nil {
		return nil, err
	}
	if opts.Dir == "" {
		dir, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("init git: %w", err)
		}
		return Custom(dir, l, l), nil
	}
	dir, err := filepath.Abs(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("init git: %w", err)
	}
	return Custom(dir, l, l), nil
}

// Custom creates a new Git context from the given Runner and FileSystem.
// It panics if the Runner is nil, the FileSystem is nil, or dir is not absolute.
// If the Runner also implements Piper, then its GitPipe method will be used for
// any large streaming operations.
func Custom(dir string, s Runner, fs FileSystem) *Git {
	if s == nil || fs == nil {
		panic("git.Custom called with nil interfaces")
	}
	if !fs.IsAbs(dir) {
		panic("git.Custom called with relative path: " + dir)
	}
	return &Git{
		dir:    fs.Clean(dir),
		runner: s,
		fs:     fs,
	}
}

// Runner returns the context's Git runner.
func (g *Git) Runner() Runner {
	return g.runner
}

// FileSystem returns the context's filesystem.
func (g *Git) FileSystem() FileSystem {
	return g.fs
}

func (g *Git) getVersion(ctx context.Context) (string, error) {
	g.versionMu.Lock()
	for g.versionCond != nil {
		c := g.versionCond
		g.versionMu.Unlock()
		select {
		case <-c:
			g.versionMu.Lock()
		case <-ctx.Done():
			return "", fmt.Errorf("git --version: %w", ctx.Err())
		}
	}
	if g.version != "" {
		// Cached version string available.
		v := g.version
		g.versionMu.Unlock()
		return v, nil
	}
	g.versionCond = make(chan struct{})
	g.versionMu.Unlock()

	// Run git --version.
	v, err := g.output(ctx, "git --version", []string{"--version"})
	g.versionMu.Lock()
	close(g.versionCond)
	g.versionCond = nil
	if err != nil {
		g.versionMu.Unlock()
		return "", err
	}
	g.version = v
	g.versionMu.Unlock()
	return v, nil
}

// Exe returns the absolute path to the Git executable.
// This method will panic if g's Runner is not of type *Local.
//
// Deprecated: Call *Local.Exe() before calling Custom.
func (g *Git) Exe() string {
	return g.runner.(*Local).Exe()
}

// WithDir returns a new instance that is changed to use dir as its working directory.
// Any relative paths will be interpreted relative to g's working directory.
func (g *Git) WithDir(dir string) *Git {
	return Custom(g.abs(dir), g.runner, g.fs)
}

func (g *Git) abs(path string) string {
	if !g.fs.IsAbs(path) {
		return g.fs.Join(g.dir, path)
	}
	return g.fs.Clean(path)
}

const (
	dataOutputLimit  = 10 << 20 // 10 MiB
	errorOutputLimit = 1 << 20  // 1 MiB
)

// Run runs Git with the given arguments. If an error occurs, the
// combined stdout and stderr will be returned in the error.
func (g *Git) Run(ctx context.Context, args ...string) error {
	return g.run(ctx, errorSubject(args), args)
}

// run runs the specified Git subcommand.  If an error occurs, the
// combined stdout and stderr will be returned in the error. run will use the
// given error prefix instead of one derived from the arguments.
func (g *Git) run(ctx context.Context, errPrefix string, args []string) error {
	output := new(bytes.Buffer)
	w := &limitWriter{w: output, n: errorOutputLimit}
	err := g.runner.RunGit(ctx, &Invocation{
		Args:   args,
		Dir:    g.dir,
		Stdout: w,
		Stderr: w,
	})
	if err != nil {
		return commandError(errPrefix, err, output.Bytes())
	}
	return nil
}

// Output runs Git with the given arguments and returns its stdout.
func (g *Git) Output(ctx context.Context, args ...string) (string, error) {
	return g.output(ctx, errorSubject(args), args)
}

// output runs the specified Git subcommand, returning its stdout.
// output will use the given error prefix instead of one derived from the arguments.
func (g *Git) output(ctx context.Context, errPrefix string, args []string) (string, error) {
	stdout := new(strings.Builder)
	stderr := new(bytes.Buffer)
	err := g.runner.RunGit(ctx, &Invocation{
		Args:   args,
		Dir:    g.dir,
		Stdout: &limitWriter{w: stdout, n: dataOutputLimit},
		Stderr: &limitWriter{w: stderr, n: errorOutputLimit},
	})
	if err != nil {
		return stdout.String(), commandError(errPrefix, err, stderr.Bytes())
	}
	return stdout.String(), nil
}

// A FileSystem manipulates paths for a possibly remote filesystem.
// The methods of a FileSystem must be safe to call concurrently.
type FileSystem interface {
	// Join joins any number of path elements into a single path.
	// Empty elements are ignored. The result must be Cleaned.
	// However, if the argument list is empty or all its elements are
	// empty, Join returns an empty string.
	Join(elem ...string) string

	// Clean returns the shortest path name equivalent to path by purely
	// lexical processing.
	Clean(path string) string

	// IsAbs reports whether the path is absolute.
	IsAbs(path string) bool

	// EvalSymlinks returns the path name after the evaluation of any
	// symbolic links. The path argument will always be absolute.
	EvalSymlinks(path string) (string, error)
}

// A Runner executes Git processes.
//
// RunGit starts a Git process with the given parameters and waits until
// the process is finished. It must not modify the Invocation.
// RunGit must be safe to call concurrently with other calls to RunGit.
//
// If the Git process exited with a non-zero exit code, there should
// be an error in its Unwrap chain that has an `ExitCode() int` method.
type Runner interface {
	RunGit(ctx context.Context, invoke *Invocation) error
}

// A Piper is an optional interface that a Runner can implement for
// more efficient streaming of long-running outputs.
//
// PipeGit starts a Git process with its standard output connected to a
// pipe. It ignores the Stdout field of the Invocation. PipeGit must be
// safe to call concurrently with other calls to PipeGit and RunGit.
//
// The returned pipe's Close method closes the pipe and then waits for
// the Git process to finish before returning. It is the caller's
// responsibility to call the Close method.
type Piper interface {
	Runner
	PipeGit(ctx context.Context, invoke *Invocation) (pipe io.ReadCloser, err error)
}

// Invocation holds the parameters for a Git process.
type Invocation struct {
	// Args is the list of arguments to Git. It does not include a leading "git"
	// argument.
	Args []string

	// Dir is an absolute directory. It's the only required field in the struct.
	Dir string

	// Env specifies additional environment variables to the Git process.
	// Each entry is of the form "key=value".
	// If Env contains duplicate environment keys, only the last
	// value in the slice for each duplicate key is used.
	// The Runner may send additional environment variables to the
	// Git process.
	Env []string

	// Stdin specifies the Git process's standard input.
	//
	// If Stdin is nil, the process reads from the null device.
	//
	// The io.Closer returned from the Runner must not return until the
	// end of Stdin is reached (any read error).
	Stdin io.Reader

	// Stdout and Stderr specify the Git process's standard output and error.
	//
	// If either is nil, Run connects the corresponding file descriptor
	// to the null device.
	//
	// The io.Closer returned from the Runner must not return until the
	// all the data is written to the respective Writers.
	//
	// If Stdout and Stderr are the same writer, and have a type that can
	// be compared with ==, at most one goroutine at a time will call Write.
	Stdout io.Writer
	Stderr io.Writer
}

// StartPipe starts a piped Git command on r. If r implements Piper, then
// r.PipeGit is used. Otherwise, StartPipe uses a fallback implementation
// that calls r.RunGit. invoke.Stdout is ignored.
func StartPipe(ctx context.Context, s Runner, invoke *Invocation) (io.ReadCloser, error) {
	if ps, ok := s.(Piper); ok {
		return ps.PipeGit(ctx, invoke)
	}
	return startPipeFallback(ctx, s, invoke)
}

func startPipeFallback(ctx context.Context, s Runner, invoke *Invocation) (io.ReadCloser, error) {
	pr, pw := io.Pipe()
	invoke2 := new(Invocation)
	*invoke2 = *invoke
	invoke2.Stdout = pw
	e := make(chan error, 1)
	go func() {
		e <- s.RunGit(ctx, invoke2)
	}()
	return localPipe{pr, func() error { return <-e }}, nil
}

// Options holds the parameters for New and NewLocal.
type Options struct {
	// Dir is the working directory to run the Git subprocess from.
	// If empty, uses this process's working directory.
	// NewLocal ignores this field.
	Dir string

	// Env specifies the environment of the subprocess.
	// If Env == nil, then the process's environment will be used.
	Env []string

	// GitExe is the name of or a path to a Git executable.
	// It is treated in the same manner as the argument to exec.LookPath.
	// An empty string is treated the same as "git".
	GitExe string

	// LogHook is a function that will be called at the start of every Git
	// subprocess.
	LogHook func(ctx context.Context, args []string)
}

// Local implements Runner by starting Git subprocesses.
type Local struct {
	exe string
	env []string // cap(env) == len(env), guaranteed by NewLocal.
	log func(context.Context, []string)
}

// NewLocal returns a new local runner with the given options.
// Dir is ignored.
func NewLocal(opts Options) (*Local, error) {
	var err error
	l := &Local{
		log: opts.LogHook,
	}

	if opts.Env == nil {
		l.env = os.Environ()
		l.env = l.env[:len(l.env):len(l.env)]
	} else {
		// Using make because append doesn't guarantee capacity.
		l.env = make([]string, len(opts.Env))
		copy(l.env, opts.Env)
	}

	if opts.GitExe == "" {
		opts.GitExe = "git"
	}
	l.exe, err = exec.LookPath(opts.GitExe)
	if err != nil {
		return nil, fmt.Errorf("init git: %w", err)
	}
	l.exe, err = filepath.Abs(l.exe)
	if err != nil {
		return nil, fmt.Errorf("init git: %w", err)
	}

	return l, nil
}

// EvalSymlinks calls path/filepath.EvalSymlinks.
func (l *Local) EvalSymlinks(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

// Join calls path/filepath.Join.
func (l *Local) Join(elem ...string) string {
	return filepath.Join(elem...)
}

// Clean calls path/filepath.Clean.
func (l *Local) Clean(path string) string {
	return filepath.Clean(path)
}

// IsAbs calls path/filepath.IsAbs.
func (l *Local) IsAbs(path string) bool {
	return filepath.IsAbs(path)
}

// command returns a new *exec.Cmd for the given invocation.
func (l *Local) command(ctx context.Context, invoke *Invocation) (*exec.Cmd, error) {
	if invoke.Dir == "" {
		return nil, fmt.Errorf("start git: directory is empty")
	}
	if !filepath.IsAbs(invoke.Dir) {
		return nil, fmt.Errorf("start git: directory %s is not absolute", invoke.Dir)
	}
	if l.log != nil {
		l.log(ctx, invoke.Args)
	}
	argv := make([]string, len(invoke.Args)+1)
	argv[0] = l.exe
	copy(argv[1:], invoke.Args)
	return &exec.Cmd{
		Path:   l.exe,
		Args:   argv,
		Env:    append(l.env, invoke.Env...),
		Dir:    invoke.Dir,
		Stdin:  invoke.Stdin,
		Stdout: invoke.Stdout,
		Stderr: invoke.Stderr,
	}, nil
}

// Exe returns the absolute path to the Git executable.
func (l *Local) Exe() string {
	return l.exe
}

// RunGit runs Git in a subprocess. If the Context is cancelled or its
// deadline is exceeded, RunGit will send SIGTERM to the subprocess.
func (l *Local) RunGit(ctx context.Context, invoke *Invocation) error {
	c, err := l.command(ctx, invoke)
	if err != nil {
		return err
	}
	return sigterm.Run(ctx, c)
}

// PipeGit starts a Git subprocess with its standard output connected to
// an OS pipe. If the Context is cancelled or its deadline is exceeded,
// PipeGit will send SIGTERM to the subprocess.
func (l *Local) PipeGit(ctx context.Context, invoke *Invocation) (io.ReadCloser, error) {
	c, err := l.command(ctx, invoke)
	if err != nil {
		return nil, err
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("start git: %w", err)
	}
	wait, err := sigterm.Start(ctx, c)
	if err != nil {
		// stdout will be automatically closed.
		return nil, err
	}
	return localPipe{stdout, wait}, nil
}

// oneLine verifies that s contains a single line delimited by '\n' and
// trims the trailing '\n'.
func oneLine(s string) (string, error) {
	if s == "" {
		return "", io.EOF
	}
	i := strings.IndexByte(s, '\n')
	if i == -1 {
		return "", io.ErrUnexpectedEOF
	}
	if i < len(s)-1 {
		return "", errors.New("multiple lines present")
	}
	return s[:len(s)-1], nil
}

func errorSubject(args []string) string {
	i := indexCommand(args)
	if i >= len(args) {
		return "git"
	}
	return "git " + args[i]
}

// indexCommand finds the index of the first non-global-option argument in a Git
// argument list or len(args) if no such argument could be found.
func indexCommand(args []string) int {
scanArgs:
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return i
		}
		if !strings.HasPrefix(a, "--") {
			// Short option. Check last character of argument.
			if strings.IndexByte(globalShortOptionsWithArgs, a[len(a)-1]) != -1 {
				i++
			}
			continue scanArgs
		}
		for _, opt := range globalLongOptionsWithArgs {
			if a[2:] == opt {
				i++
				continue scanArgs
			}
		}
	}
	return len(args)
}

var globalShortOptionsWithArgs = "cC"

var globalLongOptionsWithArgs = []string{
	"exec-path",
	"git-dir",
	"work-tree",
	"namespace",
}

type limitWriter struct {
	w io.Writer
	n int64
}

func (lw *limitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > lw.n {
		n, err := lw.w.Write(p[:int(lw.n)])
		lw.n -= int64(n)
		if err != nil {
			return n, err
		}
		return n, errors.New("buffer full")
	}
	n, err := lw.w.Write(p)
	lw.n -= int64(n)
	return n, err
}

type localPipe struct {
	io.ReadCloser
	wait func() error
}

func (p localPipe) Close() error {
	closeErr := p.ReadCloser.Close()
	waitErr := p.wait()
	if waitErr != nil {
		// Wait errors are usually more interesting than close errors.
		return waitErr
	}
	return closeErr
}
