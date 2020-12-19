# packfile/testdata

All the files in this directory are generated with [misc/genpack.go](../../misc/genpack.go).

They are then validated by running:

```shell
git init foo
cd foo
git unpack-objects --strict < DeltaOffset.pack

# git cat-file blob OBJECT to inspect
```
