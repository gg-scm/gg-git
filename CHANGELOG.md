# `gg-scm.io/pkg/git` Changelog

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.5.0][] - 2020-09-16

Version 0.5.0 added marshal/unmarshal methods to `git.Hash`.

[0.5.0]: https://github.com/gg-scm/gg-git/releases/tag/v0.5.0

### Added

-  Add marshal/unmarshal methods to `git.Hash`.
-  Now building and tested on Windows.

### Changed

-  Debian packaging is now on the `debian` branch.

## [0.4.1][] - 2020-09-02

Version 0.4.1 added Debian package release automation.

[0.4.1]: https://github.com/gg-scm/gg-git/releases/tag/v0.4.1

## [0.4.0][] - 2020-09-02

Version 0.4.0 changed the signature of a few functions and added some example
code to the documentation.

[0.4.0]: https://github.com/gg-scm/gg-git/releases/tag/v0.4.0

### Changed

-  The signature of `New` has been changed to give all parameters reasonable
   defaults. In particular, `Options.Env` now treats `nil` in the same way as
   `exec.Cmd`.
-  Renamed `Git.Path` method to `Git.Exe` to avoid confusion with `Git.WorkTree`
   and `Git.GitDir`.

## [0.3.0][] - 2020-08-28

Version 0.3.0 adds a function for parsing [Git URLs][].

[0.3.0]: https://github.com/gg-scm/gg-git/releases/tag/v0.3.0
[Git URLs]: https://git-scm.com/docs/git-fetch#_git_urls

### Added

-  Add `ParseURL` function

## [0.2.0][] - 2020-08-17

Version 0.2.0 adds functionality for inspecting [submodules][].

[0.2.0]: https://github.com/gg-scm/gg-git/releases/tag/v0.2.0
[submodules]: https://git-scm.com/book/en/v2/Git-Tools-Submodules

### Added

-  Add `Git.ListSubmodules` method

### Changed

-  The signature of `Git.ListTree` has changed to support options and parse
   full tree entries. To get the old behavior, use the `Recursive: true` and
   `NameOnly: true` options.

## [0.1.0][] - 2020-08-13

This is the first release of the `gg-scm.io/pkg/git` library outside gg.
It is identical to the [`internal/git` package][] released in [gg 1.0.1][].

[0.1.0]: https://github.com/gg-scm/gg-git/releases/tag/v0.1.0
[gg 1.0.1]: https://github.com/gg-scm/gg/releases/tag/v1.0.1
[`internal/git` package]: https://github.com/gg-scm/gg/tree/v1.0.1/internal/git
