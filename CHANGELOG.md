# `gg-scm.io/pkg/git` Changelog

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

[Unreleased]: https://github.com/gg-scm/gg-git/compare/v0.7.3...main

## [0.7.3][] - 2020-12-13

Version 0.7.3 releases minor fixes.

[0.7.3]: https://github.com/gg-scm/gg-git/releases/tag/v0.7.3

### Changed

-  `Git.Add` will no-op if passed no pathspecs.

### Fixed

-  `Git.DiffStatus` will no longer return an error on valid renames.
   ([gg-scm/gg#129](https://github.com/gg-scm/gg/issues/129))

## [0.7.2][] - 2020-10-04

Version 0.7.2 removed the Windows color no-op.

[0.7.2]: https://github.com/gg-scm/gg-git/releases/tag/v0.7.2

### Fixed

-  `*Config.Color` and `*Config.ColorBool` no longer no-op on Windows.
   ([gg-scm/gg#125](https://github.com/gg-scm/gg/issues/125))

## [0.7.1][] - 2020-10-03

Version 0.7.1 fixed an issue with working copy renames on old versions of Git.

[0.7.1]: https://github.com/gg-scm/gg-git/releases/tag/v0.7.1

### Fixed

-  Git versions before 2.18 have a bug where they will report renames even if
   `--disable-renames` is passed. `*Git.Status` will rewrite these to an add and
   a delete. ([#3](https://github.com/gg-scm/gg-git/issues/3))

## [0.7.0][] - 2020-10-02

Version 0.7 made improvements to fetching commit information.

[0.7.0]: https://github.com/gg-scm/gg-git/releases/tag/v0.7.0

### Added

-  The `LogOptions.NoWalk` field permits reading commits in bulk without having
   to process their ancestors. ([#2](https://github.com/gg-scm/gg-git/issues/2))
-  The `CommitInfo.Summary` method returns the first line of a commit message.

## [0.6.0][] - 2020-09-27

Version 0.6 introduced an interface for supplying your own Git process creation
mechanism. This was introduced with very little change in public-facing API, so
I'm fairly confident that the API can remain stable.

[0.6.0]: https://github.com/gg-scm/gg-git/releases/tag/v0.6.0

### Added

-  The Git subprocess invocation can now be customized. A new interface,
   `Runner`, allows the user to provide their own mechanism for running a Git
   subprocess. This can be used to communicate with a Git process over SSH or
   in a container, for example. ([#1](https://github.com/gg-scm/gg-git/issues/1))

### Changed

-  `GitDir` and `CommonDir` now directly use the Git subprocess to determine
   the directory paths rather than rely on kludges. This doesn't work on Git
   2.13.1 or below, but these versions have not been supported for some time.

### Deprecated

-  The `*Git.Exe` method has been deprecated in favor of the newly introduced
   `*Local.Exe`.

### Removed

-  The `*Git.Command` method has been removed because it was infeasible to
   support with the new `Runner` structure. The `Runner` interface provides
   an easier API that works across process-starting mechanisms.

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
