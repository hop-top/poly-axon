# Releasing

## How releases work

1. Conventional Commits land on `main`.
2. [release-please](.github/workflows/release-please.yml) opens or updates a
   release PR per component, titled `chore(release):<component> <version>`,
   per [`.github/release-please-config.json`](.github/release-please-config.json).
   The current prerelease stage is `alpha`, starting at `0.1.0-alpha.0`.
3. Merging a release PR tags `<component>/v<version>` and publishes a GitHub
   Release.
4. [`publish.yml`](.github/workflows/publish.yml) fires on that tag and, for
   the `axon` component, mirrors the module to
   [`hop-top/axon`](https://github.com/hop-top/axon), the repository
   `go get hop.top/axon` actually resolves against. The `spec` component
   mirrors to `hop-top/spec-axon` under `specs/v0.1/`, matching the
   `hop-top/spec-*` layout; it has no registry, so it declares
   `ecosystem: none` and runs the mirror alone. That mirror repo does not
   exist yet — it must be created before the first `spec/v*` tag, or the
   publish run fails at the mirror step.

## Components

Five independently versioned components, each with its own tag stream. Which
one a commit moves is decided by the path it touches:

| Component | Path | Tag | What it versions |
|---|---|---|---|
| `axon` | `go/` | `axon/v<version>` | the Go library, mirrored to `hop-top/axon` |
| `axon-ts` | `ts/` | `axon-ts/v<version>` | the npm package `@hop-top/axon` |
| `axon-py` | `py/` | `axon-py/v<version>` | the PyPI package `hop-top-axon` |
| `spec` | `spec/` | `spec/v<version>` | the language-neutral contract, mirrored to `hop-top/spec-axon` |
| `poly-axon` | repo root | `poly-axon/v<version>` | the monorepo itself: root docs, CI, `Makefile` |

`poly-axon` sets `exclude-paths: ["go","ts","py","spec"]`, so a commit inside
a language directory or `spec/` versions that component and not the repo's.

`spec` uses `release-type: simple`: the contract is YAML and JSON Schema with
no language manifest, so release-please tracks its version in
`.github/.release-please-manifest.json` alone. It writes a `version.txt` only
if one already exists (`createIfMissing: false`), and `spec/` has none, so
nothing is generated. `spec/version.yaml` is the contract's own version and is
hand-maintained — release-please never touches it.

There is no manual tag-and-push step and no nightly auto-merge: every
release PR is merged by a maintainer.

## Prerelease stages

```
0.1.0-alpha.0 -> alpha.1 -> ... -> 0.1.0-beta.0 -> ... -> 0.1.0-rc.0 -> ... -> 0.1.0
```

Moving between stages means editing `prerelease-type` in
`.github/release-please-config.json` (`alpha.0`, `beta.0`, `rc.0`, or
removing it for a stable release) and letting release-please pick up the
change on the next push to `main`. The `.0` suffix is required: a bare
`alpha` starts the counter at `alpha.1`.

## Binary archives

`go/.goreleaser.yml` is checked in (Linux/Darwin, amd64/arm64) but is not
wired to a workflow yet; `make release` runs it locally, from the repo root.
It lives beside the module it builds (`main: ./cmd/axon`) because goreleaser
reads its config from the working directory, which is why the target runs
`cd go && goreleaser release --clean`. Automating it is a later decision for
the module owner, independent of the tag-and-mirror flow above.

## Verifying a release

```sh
go install hop.top/axon/cmd/axon@v<version>
axon --version
```
