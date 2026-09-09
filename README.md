# axon

Host-CLI contract for AI-assistant hooks

axon is pre-1.0 (`0.x.y-alpha.N`): breaking changes are expected between
alpha releases.

<p align="center">
    <a href="https://github.com/hop-top/poly-axon/releases"><img src="https://img.shields.io/github/v/release/hop-top/poly-axon?sort=semver" alt="Release"/></a>
    <a href="https://pkg.go.dev/hop.top/axon?tab=doc"><img src="https://pkg.go.dev/badge/hop.top/axon.svg" alt="GoDoc"/></a>
    <a href="https://github.com/hop-top/poly-axon/actions/workflows/ci-go.yml"><img src="https://github.com/hop-top/poly-axon/actions/workflows/ci-go.yml/badge.svg" alt="Build Status"/></a>
    <a href="https://github.com/hop-top/poly-axon/blob/main/LICENSE"><img src="https://img.shields.io/github/license/hop-top/poly-axon" alt="License"/></a>
    <a href="https://github.com/hop-top/12-factor-ai-cli-apps"><img src="https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/hop-top/axon/main/.12fc.json" alt="12-factor AI-CLI"/></a>
</p>

## Table of contents

- [Why use it](#why-use-it)
- [When not to use it](#when-not-to-use-it)
- [Install](#install)
- [Usage](#usage)
- [Bindings](#bindings)
- [Coverage](#coverage)
- [API](#api)
- [Agent usage](#agent-usage)
- [Contributing](#contributing)
- [Security](#security)
- [License](#license)

## Why use it

Hooking into an AI-assistant CLI means learning that host's own wire
format: what it names each event, which JSON keys it sends, which exit
code means "block", and which events it does not report at all. Claude
Code, Gemini CLI, Codex, and OpenCode disagree on all four, so tooling
that targets more than one host ends up carrying a private translation
layer per host and rediscovering each host's gaps by hand. axon is that
knowledge as data plus codecs: you write against one canonical event and
decision shape, and axon translates to and from each host's native
format.

- 17 host identities — names, aliases, binaries, config paths, store
  roots, exit-code conventions — from one lookup (`axon hosts`)
- A 32-event canonical catalog, 26 of them native to at least one host,
  so you name an event once instead of per host (`axon events`)
- Capability maps that answer "does this host support this event" up
  front, classifying every event per host as native, close, synthesized,
  or unsupported — including the events a host cannot report
- JSON Schemas plus codecs that encode and decode each host's native hook
  payloads and decisions, validating raw bytes against the schema rather
  than trusting the shape
- Go, TypeScript, and Python read the same `spec/` tree, and a 71-case
  parity harness fails the build when any binding's answer diverges from
  Go's, so all three stay usable rather than one staying current

## When not to use it

axon is spec and protocol only, so it is the wrong dependency when you
want something that runs.

- **You want hooks to actually execute.** axon does not dispatch hooks,
  merge decisions from multiple handlers, manage a socket, or install
  anything into a host's config. For a dispatcher that runs one hook
  across every host, see
  [nerv](https://github.com/hop-top/poly-nerv), which speaks this
  contract.
- **You want session transcripts.** axon locates a host's store roots but
  never reads transcript files from disk or parses their contents. For
  finding and reading sessions across hosts, see
  [vein](https://github.com/hop-top/poly-vein); use axon only to resolve
  the paths.
- **You want to test a plugin against these contracts.** axon defines the
  shapes; it does not run your hook and check what it emits.
  [xat](https://github.com/hop-top/xat) is the conformance harness for
  that — one spec, run against each host's own envelope and decision
  shape.
- **You target exactly one host and always will.** A single host's own
  hook docs are a shorter path than a canonical shape you would only ever
  map one way.

## Install

```sh
go get hop.top/axon        # Go
pnpm add @hop-top/axon     # TypeScript
pip install hop-top-axon   # Python
```

The `axon` CLI, useful for inspecting the spec without writing Go:

```sh
go install hop.top/axon/cmd/axon@latest
```

**Not published yet.** axon has no releases and no tags, so none of the
commands above resolve today — `go get hop.top/axon` fails with
`no matching versions for query "latest"`, and the npm and PyPI packages do
not exist either. Until the first release, build from source: see
[`docs/manual/install.md`](docs/manual/install.md).

Requirements: Go 1.26, Node.js >= 22, or Python >= 3.11, depending on the
binding.

## Usage

Resolve a host by its canonical name or a published alias, then encode a
canonical hook input into that host's native wire format:

```go
package main

import (
	"fmt"

	"hop.top/axon"
	"hop.top/axon/hooks"
	_ "hop.top/axon/hooks/hosts" // registers every built-in codec
)

func main() {
	host, _ := axon.Resolve("claude-code") // alias for "claude"

	codec, _ := hooks.For(host.Name)
	raw, _ := codec.EncodeInput(hooks.Input{
		Event:     axon.Event("PreToolUse"),
		SessionID: "00000000-0000-4000-8000-000000000000",
		Cwd:       "/tmp/axon",
		ToolName:  "Bash",
		ToolInput: map[string]any{"command": "echo hello"},
	})
	fmt.Println(string(raw))

	if err := hooks.ValidateDecision(host.Name, axon.Event("PreToolUse"), hooks.ActionAllow, []byte("{}")); err != nil {
		panic(err)
	}
}
```

```json
{"cwd":"/tmp/axon","hook_event_name":"PreToolUse","session_id":"00000000-0000-4000-8000-000000000000","tool_input":{"command":"echo hello"},"tool_name":"Bash"}
```

The same data, from the CLI:

```sh
$ axon hosts
NAME          STATUS  HOOKS  ALIASES      BINARIES
aider         active  yes                 aider
amp           active  no                  amp
antigravity   active  no                  agy
claude        active  yes    claude-code  claude
codex         active  yes    codex-cli    codex
copilot       active  no                  copilot
crush         active  no                  crush
cursor-agent  active  no                  cursor-agent
gemini        active  yes    gemini-cli   gemini
goose         active  yes                 goose
kimi          active  no                  kimi
opencode      active  yes                 opencode
openhands     active  yes                 openhands
qwen          active  no                  qwen
tabnine       active  no                  tabnine
vibe          active  yes                 vibe
windsurf      active  no                  windsurf
```

Eight of the seventeen hosts (`hooks: yes`) carry a full capability map,
hook schemas, and fixtures; the rest are identity-only entries (name,
binaries, store roots) with no hook surface yet. See
[`docs/adding-a-host.md`](docs/adding-a-host.md) to add one.

## Bindings

One `spec/` tree is the contract; each language is a reader over it, not
a second copy of it. Go is the reference implementation; TypeScript and
Python are bindings that read the same YAML and JSON Schema files and
must produce the same answers. `make test-parity` proves this by running
all three against the same 71 cases and diffing their output against Go:
a divergence fails the build, it does not get filed as a support ticket
against whichever binding looks wrong.

Per-language install and usage: [`ts/README.md`](ts/README.md) (npm,
`@hop-top/axon`) and [`py/README.md`](py/README.md) (PyPI,
`hop-top-axon`). The Go example above is canonical; the other two READMEs
show the same operation in their own idiom.

## Coverage

Three capabilities land per host independently. **Identity** (names,
aliases, binaries, config paths, store roots, exit codes) covers all 17.
**Invoke** (building native argv) covers 11. **Hooks** covers 8, of which
4 have a codec that encodes and decodes real payloads; the other 4 have a
capability map you can query but no translation yet.

Event columns count the 26 canonical events in the host-contract subset,
classified by that host's `capabilities.yaml`: native (the host raises it
itself), close (a host event that maps onto it), synthesized (reachable
only by a bridge the host does not provide), unsupported (the host cannot
report it).

| Host | Identity | Invoke | Hooks | Codec | Native | Close | Synth | Unsup |
|---|---|---|---|---|---|---|---|---|
| `claude` | yes | yes | yes | yes | 26 | 0 | 0 | 0 |
| `opencode` | yes | yes | yes | yes | 10 | 2 | 11 | 3 |
| `gemini` | yes | yes | yes | yes | 8 | 2 | 14 | 2 |
| `codex` | yes | yes | yes | yes | 3 | 2 | 10 | 11 |
| `goose` | yes | yes | yes | — | 11 | 2 | 9 | 4 |
| `vibe` | yes | yes | yes | — | 6 | 2 | 6 | 12 |
| `openhands` | yes | — | yes | — | 5 | 1 | 14 | 6 |
| `aider` | yes | — | yes | — | 0 | 0 | 5 | 21 |
| `copilot` | yes | yes | — | — | — | — | — | — |
| `crush` | yes | yes | — | — | — | — | — | — |
| `cursor-agent` | yes | yes | — | — | — | — | — | — |
| `kimi` | yes | yes | — | — | — | — | — | — |
| `qwen` | yes | yes | — | — | — | — | — | — |
| `amp` | yes | — | — | — | — | — | — | — |
| `antigravity` | yes | — | — | — | — | — | — | — |
| `tabnine` | yes | — | — | — | — | — | — | — |
| `windsurf` | yes | — | — | — | — | — | — | — |

Read it live rather than trusting this table: `axon hosts` prints the
identity and hooks columns, and `hooks.LoadCapabilities(host)` plus
`Capabilities.Level(event)` gives the per-event classification.

Two further limits. The 4 hosts without a codec are listed in
`codecDeferred` in
[`go/hooks/conformance_test.go`](go/hooks/conformance_test.go), which
also records why: no golden envelope has been captured from those hosts,
so a codec written from the schema alone would only test axon against its
own guesses. And `invoke`'s argv builders are Go-only — the TypeScript and
Python bindings cover identity, events, capabilities, and hook codecs for
the same 8 hosts, but not `invoke`.

## API

```go
// package axon
func Hosts() []Host                                    // all registered hosts, sorted by name
func Get(name string) (Host, bool)                      // canonical name only
func Resolve(nameOrAlias string) (Host, bool)           // the only alias-aware function
func Detect(name string, opts *DetectOpts) (*DetectResult, error) // is this host installed locally?
func Spec() fs.FS                                       // the embedded spec/ tree
func Events() []EventInfo                               // full catalog, with origin and derivation
func NativeEvents() []Event                             // origin: native only

// package hooks
type Codec interface {
	Host() string
	EncodeInput(Input) ([]byte, error)
	DecodeInput([]byte) (Input, error)
	EncodeDecision(Decision) (stdout []byte, exit int, err error)
	DecodeDecision(stdout []byte, exit int) (Decision, error)
	Capabilities() Capabilities
}
func For(host string) (Codec, bool)
func ValidateInput(host string, ev axon.Event, raw []byte) error
func ValidateDecision(host string, ev axon.Event, action Action, raw []byte) error
var ErrUnknownHost, ErrUnsupportedEvent, ErrSchema error

// package invoke
type InvocationAdapter interface {
	CLI() string
	Build(inv Invocation) (CommandSpec, Diagnostics, error)
}
// one adapter per host under invoke/adapters/<host>: claude.New().Build(inv)
```

Full reference: [pkg.go.dev/hop.top/axon](https://pkg.go.dev/hop.top/axon).
Invocation parity tables (which flags each host CLI supports natively,
which are shimmed, which are refused): [`go/invoke/README.md`](go/invoke/README.md).
Spec file shapes and the versioning rule: [`spec/README.md`](spec/README.md).

## Agent usage

`axon` never prompts and every command accepts `--format json`.

```sh
axon events --format json | head -c 200
```

```json
[
  {
    "Name": "SessionStart",
    "Category": "session",
    "Description": "Session begins or resumes.",
    "Direction": "outbound",
    "Blocking": false,
    "Origin": "native",
    "ExtensionSource": "",
    "Derivation": null
  },
  ...
```

Exit codes: 0 success, 1 runtime error, 2 usage error (unknown host,
event, or action; bad arguments).

## Contributing

Questions and bug reports: open an issue. See
[`CONTRIBUTING.md`](CONTRIBUTING.md) for the development setup, and
[`docs/adding-a-host.md`](docs/adding-a-host.md) for the steps and tests
that gate a new host. Run `make check` before submitting.

## Security

Report a vulnerability per [`SECURITY.md`](SECURITY.md).

## License

MIT, see [LICENSE](LICENSE).
