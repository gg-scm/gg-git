# packfile/testdata

All the files in this directory are generated with [misc/genpack.go](../../misc/genpack.go).

They are then validated by running:

```shell
git init foo &&
cd foo &&
git unpack-objects --strict < ${packfile?}.pack

# git cat-file blob OBJECT to inspect
```

Another helpful debugging sequence to list file contents:

```shell
git index-pack ${packfile?}.pack &&
git verify-pack -v ${packfile?}.idx
```

See [git-verify-pack man page](https://git-scm.com/docs/git-verify-pack) for
details on output format.
