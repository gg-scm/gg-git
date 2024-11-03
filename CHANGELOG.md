# `gg-scm.io/pkg/git` Changelog

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

[Unreleased]: https://github.com/gg-scm/gg-git/compare/v0.11.0...main

## [Unreleased][]

### Changed

- The `io/fs` package is used for `FileMode` and `FileInfo` instead of `os`.
  This should be a compatible change, since the `os` types are aliases.
- `*object.Commit.MarshalBinary` will use `*time.Location` names if they are in the proper format.
  ([#34](https://github.com/gg-scm/gg-git/pull/34))

### Fixed

- Using `packfile/client` to read from an empty repository on old versions (~2.17) of Git
  no longer returns an error.
- `*object.Commit.UnmarshalBinary` accepts timezones with less than 4 digits.
  ([#34](https://github.com/gg-scm/gg-git/pull/34))
- `object.Tree` now orders directories correctly.
  (Thanks to [@yangchi](https://github.com/yangchi) for discovering this issue.)
- `*Config.ListRemotes` matches behavior when running with Git 2.46+.

## [0.11.0][] - 2023-08-01

Version 0.11 adds an iteration API for refs.

[0.11.0]: https://github.com/gg-scm/gg-git/releases/tag/v0.11.0

### Added

- New methods `Git.IterateRefs` and `Git.IterateRemoteRefs`
  provide a streaming API for listing refs.

### Deprecated

- `Git.ListRefs` and `Git.ListRefsVerbatim` are deprecated
  in favor of `Git.IterateRefs`.
- `Git.ListRemoteRefs` is deprecated in favor of `Git.IterateRemoteRefs`.

### Fixed

- `Log` no longer retains data after closing.
- Fixed a panic in `packfile/client.Remote.StartPush`.

## [0.10.0][] - 2022-02-22

Version 0.10 adds several features for mutating refs in a working copy
and correctly handles extra fields in commit objects.

[0.10.0]: https://github.com/gg-scm/gg-git/releases/tag/v0.10.0

### Added

- There are several new `git.RefMutation` constructors: `SetRef`,
  `SetRefIfMatches`, and `CreateRef`.
- `git.RefMutation` has a new `IsNoop` method to make it easier to check for
  the zero value.
- `git.CommitOptions` and `git.AmendOptions` have a new field: `SkipHooks`.
- New method `Git.DeleteBranches`.
- `object.Commit` has a new field `Extra` that stores any additional commit fields.

### Changed

- `*client.PullStream.ListRefs` and `*client.PushStream.Refs` now return a map
  of refs instead of a slice.

### Fixed

- `object.ParseCommit` no longer rejects commits with extra fields.
  ([#23](https://github.com/gg-scm/gg-git/issues/23))

## [0.9.0][] - 2021-01-26

Version 0.9 adds a new package for interacting with remote Git repositories and
expands the `packfile` package to handle random access.

[0.9.0]: https://github.com/gg-scm/gg-git/releases/tag/v0.9.0

### Added

-  A new `packfile/client` package enables downloading commits from and
   uploading commits to remote Git repositories.
   ([#7](https://github.com/gg-scm/gg-git/issues/7))
-  The `packfile.DeltaReader` type is a flexible way of expanding a deltified
   object from a packfile.
-  The `packfile.Undeltifier` type decompresses objects from packfiles.
-  The `packfile.Index` type stores a packfile object ID lookup table that is
   interoperable with Git packfile index files.
   ([#12](https://github.com/gg-scm/gg-git/issues/12))
-  `packfile.ReadHeader` enables random access to a packfile.
-  `*object.Commit` and `*object.Tag` now implement `BinaryMarshaler` and
   `BinaryUnmarshaler` in addition to `TextMarshaler` and `TextUnmarshaler`.
   This is for symmetry with `object.Tree`.
-  `object.Prefix` allows marshaling and unmarshaling the `"blob 42\x00"` prefix
   used as part of the Git object hash.
-  The new `*Git.Clone` and `*Git.CloneBare` methods clone repositories.
-  `git.URLFromPath` converts a filesystem path into a URL.

### Changed

-  The `githash` package is now the home for the `Ref` type. This permits
   ref string manipulation without depending on the larger `git` package.
   `git.Ref` is now a type alias for `githash.Ref`.

### Removed

-  Removed the `packfile.ApplyDelta` function. The `packfile.DeltaReader` type
   performs the same function but permits more control over how it's used.

### Fixed

-  `Ref.IsValid` produces less false positives than before.
   ([#16](https://github.com/gg-scm/gg-git/issues/16))

## [0.8.1][] - 2021-01-02

Version 0.8.1 updates the README.

[0.8.1]: https://github.com/gg-scm/gg-git/releases/tag/v0.8.1

## [0.8.0][] - 2020-12-31

Version 0.8 adds two new packages: `object` and `packfile`. These are pure Go
implementations of the Git data structures and wire protocol, respectively.
While most users will not interact with these packages directly, this provides
better correctness guarantees, and makes it easier to directly read or write
objects to a Git repository. Users that inspect commits with the `git` package
now receive a higher fidelity data structure than before.

[0.8.0]: https://github.com/gg-scm/gg-git/releases/tag/v0.8.0

### Added

-  A new `object` package enables serializing and deserializing Git objects.
-  A new `packfile` package enables reading and writing the Git packfile format.
   ([#4](https://github.com/gg-scm/gg-git/issues/4))

### Changed

-  `*Git.CommitInfo` and `*Log.CommitInfo` now return an `*object.Commit`
   instead of a `*git.CommitInfo`.
-  `CommitOptions.Author`, `CommitOptions.Committer`, `AmendOptions.Author` and
   `AmendOptions.Committer` are now type `object.User` instead of `git.User`.
-  `*Git.Log` now calls `git rev-list | git cat-file` instead of `git log` and
   parses the commits directly. One slight semantic change: if `HEAD` does not
   exist (an empty repository), then `*Log.Close` returns an error.
-  `*TreeEntry.Mode` now sets both `os.ModeDir` and `os.ModeSymlink` for
   submodules. This is more consistent with how Git treats these entries
   internally.
-  `*TreeEntry.ObjectType` now returns an `object.Type` instead of a `string`.
   It is otherwise functionally identical.

### Removed

-  `git.CommitInfo` has been removed in favor of `object.Commit`. The latter has
   mostly the same fields as the former, but does not contain a `Hash` field
   because the hash can be computed in Go.
-  `git.User` has been removed in favor of `object.User`. The latter is a string
   rather than a struct so as to pass through user information from Git objects
   verbatim.

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
