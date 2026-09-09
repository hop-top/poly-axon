# Architecture: axon

axon is a spec artifact first and a set of language readers second: the
root `spec/` tree is the source of truth, and the three bindings — `go/`,
`ts/`, `py/` — each embed or bundle it and expose typed access to that data
plus the behaviour data cannot express (wire-format translation). This is
orientation, not a tour; see each language directory's own README and
[`docs/adding-a-host.md`](adding-a-host.md) for the work of extending it.

## Directory structure

No language owns the repo root: `spec/` is language-neutral and each
binding lives in its own directory.

```
.
├── spec/            # canonical data: host identities, capability maps, hook
│                     # schemas, fixtures (see spec/README.md) -- read by
│                     # all three languages, owned by none of them
├── go/              # the Go module (module hop.top/axon)
│   ├── spec/        # GENERATED, committed mirror of ../spec -- see below
│   ├── *.go         # Go root package: Host, Hosts, Get, Resolve, Detect,
│   │                 # ResolveStorePath, DeriveKey, Events, Spec
│   ├── hooks/       # Input, Decision, Codec, For, Register, Validate*,
│   │                 # Capabilities; hooks/hosts/<name> codec implementations
│   ├── invoke/      # Go-only: native argv builders per host CLI (adapters/<name>)
│   ├── internal/gen/ # codegen: events.yaml -> events_gen.go, hosts ->
│   │                 # hosts_gen.go, invoke parity README, the spec/ mirror
│   ├── cmd/axon/    # Go CLI entry point
│   └── tools/parity/ # the Go parity emitter (imports hop.top/axon)
├── ts/              # TypeScript binding (@hop-top/axon): same surface as
│                     # the Go root package and hooks, minus invoke
├── py/              # Python binding (hop-top-axon): same surface as ts/
├── tools/parity/    # parity orchestrator, cases.json, and the ts/py emitters
└── docs/            # this file, adding-a-host.md, spec-contract-notes.md, manual/
```

The import path is unaffected by the directory: `go.mod` declares
`module hop.top/axon`, so consumers import `hop.top/axon`,
`hop.top/axon/hooks`, and `hop.top/axon/invoke` regardless of where the
module sits in this repo.

## Root `spec/` is canonical; `go/spec/` is generated

There are two copies of the spec tree, and the distinction matters before
you edit either. Root `spec/` is the canonical, language-neutral contract —
the only one to edit. `go/spec/` is a generated, committed mirror of it;
hand-editing the mirror is always wrong.

The mirror exists because of two constraints that together rule out every
alternative. `//go:embed` cannot traverse upward out of its module
directory, so a directive in `go/` cannot reach `../spec`. And unlike the
TypeScript and Python bindings — which copy the tree in at build time
(`pnpm build`, wheel `force-include`) — a consumer running
`go get hop.top/axon` never executes this repo's Makefile, so the embedded
tree has to already be present in the published module. That is why the
copy is committed rather than built, and why it cannot be a symlink
(`go:embed` refuses to follow them).

`make generate` regenerates it; `make generate-check` fails on any added,
modified, or deleted file in the mirror, and is part of `make check` — so
edit root `spec/`, then run `make generate` before committing. The
duplication is a deliberate, gated cost, recorded in
[`spec/README.md`](../spec/README.md).

## Three readers, one reference

Go, TypeScript, and Python each read `spec/` independently; none of them
transcribes the others' data into source. Go is the reference
implementation, not one vote among three: `tools/parity/parity.py` names
it in `REFERENCE`, next to the `LANGUAGES` table, and compares every
other language's output for a case against Go's. When a case diverges,
the harness reports it as that other language disagreeing with the
reference, not as an even split to adjudicate. A change to `ts/` or
`py/` that happens to make them agree with each other but not with Go is
a failure rather than a majority verdict.

That is an enforced invariant, independent of table order. A binding can
never be promoted into the reference role by Go's absence: if the
reference fails to build, `build_emitters()` aborts the run; if
`REFERENCE` names a language missing from `LANGUAGES`,
`require_reference()` rejects it; and if Go returns `unsupported` for a
case that any other language answered, the run fails on that case. All
three are setup errors (exit 2), distinct from a divergence (exit 1). A
case that *no* language answers remains a legitimate skip.

`make test-parity` runs all three against the same case set (71 cases as
of this writing, see [Bindings in the root README](../README.md#bindings)
for the current count) and fails the build on any divergence. The
orchestrator and the case set stay at the repo root
(`tools/parity/parity.py`, `cases.json`); the Go emitter lives inside the
module at `go/tools/parity/` because it imports `hop.top/axon`. Only the
*build* moves with the language — every emitter is run from the repo root,
because the frozen contract has each one read `tools/parity/cases.json` by
that relative path. See
[`tools/parity/README.md`](../tools/parity/README.md) for the emitter
contract a new language or new case must satisfy.

## Data flow (Go reference)

TypeScript and Python each follow the same shape in their own idiom
(bundling `spec/` at build time rather than `go:embed`, a per-language
codec registry rather than Go's `init()` side effect); this section
describes the Go reference implementation specifically.

1. `go/spec/hosts/<name>/*.yaml` and `go/spec/fixtures/**` — the generated
   mirror of the canonical root tree — are embedded via `go:embed` in
   `go/spec.go` and exposed as an `fs.FS` through `Spec()`.
2. The root Go package (`go/registry.go`, `go/host.go`, `go/events.go`)
   parses `host.yaml` and `events.yaml` into typed `Host` and `Event` values.
3. `go/hooks/hosts/all.go` imports every host's codec package, which
   registers itself with `hooks.Register` in an `init()`. A consumer that
   imports one host package alone gets only that codec.
4. A `Codec` translates a host's native hook wire format to/from the
   canonical `hooks.Input` / `hooks.Decision`, using `Capabilities()`
   (parsed `capabilities.yaml`) and `host.yaml`'s `exit_codes` rather than
   literals.
5. `hooks.ValidateInput` / `ValidateDecision` check raw bytes against the
   embedded JSON Schemas under `go/spec/hosts/<name>/hooks/`.
6. `go/cmd/axon` is a thin cobra CLI over the same packages; it adds no
   behaviour of its own beyond argument parsing and output formatting.

## Key design decisions

- **Static registration, one place**: `go/hooks/hosts/all.go` is the only
  file with hook-codec `init()` side effects.
- **Data before code**: `Capabilities()` returns parsed YAML; codecs add
  behaviour only.
- **Nothing falls back silently**: an unknown host or event is always an
  error (`ErrUnknownHost`, `ErrUnsupportedEvent`, `ErrSchema`).
- **No dependency on `hop.top/kit`**: enforced by
  `TestGoModHasNoKit` in `go/hooks/conformance_test.go`.

See [`spec/README.md`](../spec/README.md) for the spec tree's file shapes
and versioning rule, and [`docs/adding-a-host.md`](adding-a-host.md) for
the steps and tests that gate a new host.
