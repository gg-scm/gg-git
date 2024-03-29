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
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"gg-scm.io/pkg/git/internal/filesystem"
	"github.com/google/go-cmp/cmp"
)

var _ Piper = new(Local)

func TestLocalCommand(t *testing.T) {
	gitPath, err := findGit()
	if err != nil {
		t.Skip("git not found:", err)
	}
	tests := []struct {
		name    string
		env     []string
		wantEnv []string
	}{
		{
			name:    "NilEnv",
			env:     nil,
			wantEnv: os.Environ(),
		},
		{
			name:    "EmptyEnv",
			env:     []string{},
			wantEnv: []string{},
		},
		{
			name:    "FooEnv",
			env:     []string{"FOO=bar"},
			wantEnv: []string{"FOO=bar"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			var hookArgs []string
			var env []string
			if test.env != nil {
				env = append([]string{}, test.env...)
			}
			l, err := NewLocal(Options{
				GitExe: gitPath,
				Env:    env,
				LogHook: func(_ context.Context, args []string) {
					hookArgs = append([]string(nil), args...)
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			c, err := l.command(ctx, &Invocation{
				Dir:  dir,
				Args: []string{"commit", "-m", "Hello, World!"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if c.Path != gitPath {
				t.Errorf("c.Path = %q; want %q", c.Path, gitPath)
			}
			if len(c.Args) == 0 {
				t.Error("len(c.Args) = 0; want 4")
			} else {
				if got, want := filepath.Base(c.Args[0]), filepath.Base(gitPath); got != want {
					t.Errorf("c.Args[0], filepath.Base(c.Args[0]) = %q, %q; want %q, %q", c.Args[0], got, gitPath, want)
				}
				if got, want := c.Args[1:], ([]string{"commit", "-m", "Hello, World!"}); !cmp.Equal(got, want) {
					t.Errorf("c.Args[1:] = %q; want %q", got, want)
				}
			}
			if !cmp.Equal(c.Env, test.wantEnv) {
				t.Errorf("c.Env = %#v; want %#v", c.Env, test.wantEnv)
			}
			if c.Dir != dir {
				t.Errorf("c.Dir = %q; want %q", c.Dir, dir)
			}
			if want := ([]string{"commit", "-m", "Hello, World!"}); !cmp.Equal(hookArgs, want) {
				t.Errorf("log hook args = %q; want %q", hookArgs, want)
			}
		})
	}
}

func TestRun(t *testing.T) {
	gitPath, err := findGit()
	if err != nil {
		t.Skip("git not found:", err)
	}
	ctx := context.Background()
	env, err := newTestEnv(ctx, gitPath)
	if err != nil {
		t.Fatal(err)
	}
	defer env.cleanup()

	if err := env.g.Run(ctx, "init", "repo"); err != nil {
		t.Error("Run:", err)
	}
	gitDir := env.root.FromSlash("repo/.git")
	info, err := os.Stat(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Errorf("%s is not a git directory", gitDir)
	}
}

func TestOutput(t *testing.T) {
	gitPath, err := findGit()
	if err != nil {
		t.Skip("git not found:", err)
	}
	ctx := context.Background()
	env, err := newTestEnv(ctx, gitPath)
	if err != nil {
		t.Fatal(err)
	}
	defer env.cleanup()

	if err := env.g.Run(ctx, "init"); err != nil {
		t.Fatal(err)
	}
	out, err := env.g.Output(ctx, "config", "core.bare")
	if out != "false\n" || err != nil {
		t.Errorf("Output(ctx, \"config\", \"core.bare\") = %q, %v; want \"false\\n\", <nil>", out, err)
	}
}

func TestIndexCommand(t *testing.T) {
	tests := []struct {
		args []string
		want int
	}{
		{
			args: nil,
			want: 0,
		},
		{
			args: []string{"diff"},
			want: 0,
		},
		{
			args: []string{"diff", "-p"},
			want: 0,
		},
		{
			args: []string{"-p", "diff", "-p"},
			want: 1,
		},
		{
			args: []string{"-C", "foo", "diff", "-p"},
			want: 2,
		},
		{
			args: []string{"-pC", "foo", "diff", "-p"},
			want: 2,
		},
		{
			args: []string{"--work-tree", "foo"},
			want: 2,
		},
		{
			args: []string{"--work-tree=foo", "foo"},
			want: 1,
		},
		{
			args: []string{"--work-treex", "foo"},
			want: 1,
		},
		{
			args: []string{"--work-treex", "foo", "bar"},
			want: 1,
		},
	}
	for _, test := range tests {
		if got := indexCommand(test.args); got != test.want {
			t.Errorf("indexCommand(%q) = %d; want %d", test.args, got, test.want)
		}
	}
}

type testEnv struct {
	top  filesystem.Dir
	root filesystem.Dir
	g    *Git
}

func newTestEnv(ctx context.Context, gitExe string) (*testEnv, error) {
	topPath, err := os.MkdirTemp("", "gg_git_test")
	if err != nil {
		return nil, err
	}
	// Always evaluate symlinks in the root directory path so as to make path
	// comparisons easier (simple equality). This is mostly relevant on macOS.
	topPath, err = filepath.EvalSymlinks(topPath)
	if err != nil {
		return nil, err
	}
	top := filesystem.Dir(topPath)
	if err := top.Apply(filesystem.Mkdir("scratch")); err != nil {
		os.RemoveAll(topPath)
		return nil, err
	}
	root := filesystem.Dir(top.FromSlash("scratch"))
	g, err := New(Options{
		GitExe: gitExe,
		Dir:    root.String(),
		Env: []string{
			"GIT_CONFIG_NOSYSTEM=1",
			"HOME=" + topPath,
			"TERM=xterm-color", // stops git from assuming output is to a "dumb" terminal
		},
	})
	if err != nil {
		os.RemoveAll(topPath)
		return nil, err
	}
	const miniConfig = "[user]\nname = User\nemail = foo@example.com\n"
	if err := top.Apply(filesystem.Write(".gitconfig", miniConfig)); err != nil {
		os.RemoveAll(topPath)
		return nil, err
	}
	return &testEnv{top: top, root: root, g: g}, nil
}

func (env *testEnv) cleanup() {
	os.RemoveAll(env.top.String())
}

// prettyCommit annotates the hex-encoded hash with a name if present
// in the given map.
func prettyCommit(h Hash, names map[Hash]string) string {
	n := names[h]
	if n == "" {
		return h.String()
	}
	return h.String() + " (" + n + ")"
}

var gitPathCache struct {
	mu  sync.Mutex
	val string
}

func findGit() (string, error) {
	defer gitPathCache.mu.Unlock()
	gitPathCache.mu.Lock()
	if gitPathCache.val != "" {
		return gitPathCache.val, nil
	}
	path, err := exec.LookPath("git")
	if err != nil {
		return "", err
	}
	gitPathCache.val = path
	return path, nil
}

// dummyContent is a non-empty string that is used in tests where the
// exact data is not relevant to the test.
const dummyContent = "Hello, World!\n"
