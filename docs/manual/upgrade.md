# Upgrading axon

**Not published yet.** axon has no releases and no tags, so there is nothing
to upgrade from yet and the commands below do not resolve. They describe the
flow once the first release lands.

## Library

```sh
go get -u hop.top/axon
```

## CLI

```sh
go install hop.top/axon/cmd/axon@latest
```

## Compatibility

Canonical host names never change once published, aliases are never
removed, and host directories are never deleted (see
[`spec/README.md`](../../spec/README.md)). Check the version's entry in
[`CHANGELOG.md`](../../CHANGELOG.md) for anything else that changed.

axon is pre-1.0 (`0.x.y-alpha.N`): breaking changes are expected between
alpha releases.
