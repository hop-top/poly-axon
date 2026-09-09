# spec/

The data-first source of truth for host identities, hook capabilities, and
the canonical event taxonomy. This tree is language-neutral and owned by no
binding; each one embeds or reads it (Go via `go:embed` in `go/spec.go`,
over the generated `go/spec/` mirror described below; `ts/` copies it to
`ts/dist/spec/` at `pnpm build` time; `py/` force-includes it into the
wheel at `axon/spec/`), and every file is validated against a JSON Schema
(draft-07) in tests.

## This tree is canonical; `go/spec/` is generated

Root `spec/` is the canonical tree. `go/spec/` is a generated, committed
mirror of it — **never hand-edit the copy**; edit here and run
`make generate` (or `go generate ./...` from `go/`). `make generate-check`
fails on any modified, deleted, or added file in the mirror.

The mirror exists because `//go:embed` cannot traverse upward out of the
module directory, and a consumer running `go get hop.top/axon` never
executes this repo's Makefile, so the embedded tree has to be committed
inside the module. The duplication is a deliberate, gated cost.

## The bindings rule

`spec/` is language-neutral: nothing here is Go-specific, and every file
is meant to be read by more than one language. `spec/` is the single
source of truth. A binding (TypeScript's `ts/`, Python's `py/`, or any
future language) reads these files at build or run time; it never
hand-transcribes their contents into its own source. Generated mirrors
are permitted where a check gates the drift: `events_gen.go` and
`ts/src/events_gen.ts` are tracked files holding the event names as
literals, and `make generate-check` fails the build if either drifts from
`events.yaml`. Adding a language means adding a reader plus a parity
emitter (under `tools/parity/emitters/`, or inside the language's own
directory when it must compile against that language's package, as
`go/tools/parity/` does), never hand-copying event names, host names,
capability rows, or exit codes into that language's source: those values
change here, not there.

The two files below are named as repo-root-relative paths rather than
linked, because this file is copied verbatim into the generated `go/spec/`
mirror: a relative link would resolve to a different depth from there, and
in the published Go module the repo's `docs/` and `tools/` trees are absent
altogether. Read `docs/spec-contract-notes.md` for the field-level rules an
implementer needs (key casing, Go-shaped zero values, the native-event
subset, and more), and `tools/parity/README.md` for the harness contract a
new reader must satisfy.

## Files

- `version.yaml` — spec tree version, independent of the Go module version.
- `events.yaml` — canonical event catalog, seeded verbatim from nerv.
- `events.schema.json` — schema for `events.yaml`.
- `host.schema.json` — schema for `hosts/<name>/host.yaml`.
- `capabilities.schema.json` — schema for `hosts/<name>/capabilities.yaml`.
- `invoke.schema.json` — schema for `hosts/<name>/invoke.yaml`.
- `hosts/<name>/host.yaml` — identity: name, aliases, binaries, store roots,
  config paths, project-key strategy, hook config files, exit-code
  convention, envelope discriminator field, status.
- `hosts/<name>/capabilities.yaml` — native / close / synthesized /
  unsupported per canonical event.
- `hosts/<name>/invoke.yaml` — option mappings and tool taxonomy. Generated
  from the Go `invoke` package's adapters (`go/internal/gen/invoke`); never
  hand-edited, and a binding treats it as read-only.
- `hosts/<name>/hooks/*.schema.json` — per-event input/decision schemas.
- `fixtures/hosts/<name>/*.json` — golden native envelopes and decisions.

## Versioning

`version.yaml` bumps on any change to `events.yaml` or any `*.schema.json`.
Adding a host, a fixture, or a capability row does not bump it. Semver:
removing or renaming an event or a required schema field is major.

## Contribution unit

A host directory is the unit of contribution. Adding a host means one
`hosts/<name>/` directory plus one Go codec file. The conformance test
names every missing file.

## Aliases

Aliases exist because long names live in data axon does not control:
transcript envelopes on disk and hand-written `cli:` fields in other
tools' configs. A host's `host.yaml` lists these under `aliases[]`.

- An alias is admitted only when a published artifact uses it (initial
  set: `claude-code`, `gemini-cli`, `codex-cli`).
- Aliases are resolved in exactly one function, `axon.Resolve`. Nothing
  else in axon, and no Go constant, directory name, or emitted value,
  ever uses an alias.
- `name` stays the canonical identifier used everywhere else in the spec
  tree; aliases never introduce a second directory.
- An alias is never removed once published.

## Retiring a host

Never delete a host directory. Mark it `status: retired` in `host.yaml`
instead, so consumers keep resolving historical fixtures and capability
rows for that host.

## Schema dialect

All schemas are JSON Schema draft-07. Each carries a top-level
`required[]`.

## Capability shape

Every `capabilities.yaml` validates against one shape:
`native` (host event to canonical event), `close` (same, plus a `note`),
`synthesized` (canonical event, `source` host event, `pattern`, and the
optional `technique` and `cost`), `unsupported`.

`technique` and `cost` are optional because only nerv's codex adapter
states them. Their absence means nerv does not classify that row; nothing
is inferred from it.
