# Installing axon

**Not published yet.** axon has no releases and no tags: `go get
hop.top/axon` currently fails with `no matching versions for query
"latest"`, and there is no npm or PyPI package. The two sections below
describe how installation will work once the first release lands; until
then, use [From source](#from-source).

## As a library

```sh
go get hop.top/axon
```

## As a CLI

```sh
go install hop.top/axon/cmd/axon@latest
```

## From source

Every `make` target runs from the repo root, not from `go/`; the Makefile
enters the module itself (`go -C go ...`).

```sh
git clone https://github.com/hop-top/poly-axon
cd poly-axon
mise install    # pinned Go toolchain and lint/link tools
make build      # writes ./bin/axon
```

Optionally put it on `$PATH`:

```sh
make symlink
```

There is no package-manager formula and no prebuilt binaries yet:
`go/.goreleaser.yml` exists but is not wired to a release job. See
[`RELEASING.md`](../../RELEASING.md).

## Verify installation

```sh
axon --version
```

## Requirements

Go 1.26, pinned in `mise.toml`.
