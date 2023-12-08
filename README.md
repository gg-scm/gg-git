# `gg-scm.io/pkg/git`

[![Reference](https://pkg.go.dev/badge/gg-scm.io/pkg/git)](https://pkg.go.dev/gg-scm.io/pkg/git)
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-v2.0%20adopted-ff69b4.svg)](CODE_OF_CONDUCT.md)

`gg-scm.io/pkg/git` provides a high-level interface for interacting with a
[Git][] subprocess in Go. It was developed for [gg][], but this library is
useful for any program that wishes to interact with a Git repository.
`gg-scm.io/pkg/git` lets your Go program read information from commit history,
create new commits, and do anything else you can do from the Git CLI.

[Git]: https://git-scm.com/
[gg]: https://gg-scm.io/

## Installation

Inside your Go project, run:

```
go get gg-scm.io/pkg/git
```

## Usage

```go
// Find the Git executable.
g, err := git.New(git.Options{})
if err != nil {
  return err
}

// Write a file and track it with `git add`.
err = os.WriteFile("foo.txt", []byte("Hello, World!\n"), 0o666)
if err != nil {
  return err
}
err = g.Add(ctx, []git.Pathspec{git.LiteralPath("foo.txt")}, git.AddOptions{})
if err != nil {
  return err
}

// Create a new commit.
err = g.Commit(ctx, "Added foo.txt with a greeting", git.CommitOptions{})
if err != nil {
  return err
}
```

See more examples [on pkg.go.dev](https://pkg.go.dev/gg-scm.io/pkg/git#pkg-examples).

This library is tested against Git 2.17.1 and newer. Older versions may work,
but are not supported.

## Support

If you've found an issue, file it on the [issue tracker][].

[issue tracker]: https://github.com/gg-scm/gg-git/issues

## Motivation

As [noted in the Git book][Embedding Git], shelling out to Git has the benefit
of using the canonical implementation of Git and having all of its features.
However, as the book notes, trying to interact with a Git subprocess requires
"pars\[ing] Git's occasionally-changing output format to read progress and result
information, which can be inefficient and error-prone." This package handles all
those details for you. Common operations like `git commit` or `git rev-parse`
are wrapped as functions that take in Go data structures as input and return
Go data structures as output. These are methods are tested to be robust over
many different versions of Git under different scenarios. For less common
operations, you can [invoke Git][] with exactly the same command line arguments
you would use interactively and `gg-scm.io/pkg/git` will handle finding Git and
collecting error messages for you. All Git subprocess invocations can be
[logged][LogHook] for easy debugging.

The goals of `gg-scm.io/pkg/git` are different from those of go-git. go-git
aims to be a reimplementation of Git in pure Go. go-git avoids the dependency
on the Git CLI, but as a result, go-git does not have the same feature set as
upstream Git and introduces the potential for discrepancies in
implementation. `gg-scm.io/pkg/git` is intended for scenarios where the
fidelity of interactions with Git matters.

[Embedding Git]: https://git-scm.com/book/en/v2/Appendix-B%3A-Embedding-Git-in-your-Applications-Command-line-Git
[invoke Git]: https://pkg.go.dev/gg-scm.io/pkg/git#Git.Run
[LogHook]: https://pkg.go.dev/gg-scm.io/pkg/git#Options.LogHook

## Stability

The following packages are stable and we make a reasonable effort to avoid
backward-incompatible changes:

-  `gg-scm.io/pkg/git`
-  `gg-scm.io/pkg/git/githash`

The following packages are relatively new and may still make breaking changes:

-  `gg-scm.io/pkg/git/object`
-  `gg-scm.io/pkg/git/packfile`
-  `gg-scm.io/pkg/git/packfile/client`

Because we still have some packages in early development, we have kept the
entire repository on major version 0. When all packages are stable, we will
start using major version 1.

## Contributing

We'd love to accept your patches and contributions to this project.
See [CONTRIBUTING.md](CONTRIBUTING.md) for more details.

If you find this package useful, consider [sponsoring @zombiezen][],
the author and maintainer.

[sponsoring @zombiezen]: https://github.com/sponsors/zombiezen

## Links

-  [gg website](https://gg-scm.io/)
-  [Sponsor](https://github.com/sponsors/zombiezen)

## License

[Apache 2.0](LICENSE)
