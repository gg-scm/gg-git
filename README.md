# `gg-scm.io/pkg/git`

[![Reference](https://pkg.go.dev/badge/gg-scm.io/pkg/git?tab=doc)](https://pkg.go.dev/gg-scm.io/pkg/git?tab=doc)
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-v2.0%20adopted-ff69b4.svg)](CODE_OF_CONDUCT.md)

`gg-scm.io/pkg/git` provides a high-level interface for interacting with a
[Git][] subprocess in Go. It was developed for [gg][], but this library is
useful for any program that wishes to interact with a Git repository.
This library is tested against Git 2.20.1 and newer. Older versions may work,
but are not supported.

If you find this package useful, consider [sponsoring @zombiezen][],
the author and maintainer.

[Git]: https://git-scm.com/
[gg]: https://gg-scm.io/
[sponsoring @zombiezen]: https://github.com/sponsors/zombiezen

## Installation

```
go get gg-scm.io/pkg/git
```

## License

[Apache 2.0](LICENSE)
