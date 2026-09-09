# axon (go)

Hooking into an AI-assistant CLI means learning that host's own wire
format: its event names, its JSON keys, its exit-code convention, and
which events it never reports. Claude Code, Gemini CLI, Codex, and
OpenCode disagree on all four. This module gives you one canonical event
and decision shape plus the codecs that translate to and from each host's
native format, read directly from the shared `spec/` tree. It is the Go
reference implementation of `hop.top/axon`: the TypeScript and Python
bindings are checked against its answers, never the reverse.

> This repository is a read-only language mirror, republished from the
> `hop-top/poly-axon` monorepo on each release. Open issues and pull
> requests in [`hop-top/poly-axon`](https://github.com/hop-top/poly-axon).

## Install

```sh
go get hop.top/axon
```

The `axon` CLI, for inspecting the spec without writing Go:

```sh
go install hop.top/axon/cmd/axon@latest
```

Requirements: Go 1.26.

**Not published yet.** axon has no releases and no tags, so neither
command resolves today — `go get hop.top/axon` fails with
`no matching versions for query "latest"`. Until the first release, build
from source: see
[`docs/manual/install.md`](https://github.com/hop-top/poly-axon/blob/main/docs/manual/install.md).

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
	host, ok := axon.Resolve("claude-code") // alias for "claude"
	codec, hasCodec := hooks.For(host.Name)
	if !ok || !hasCodec {
		panic("unknown host or no codec registered")
	}

	raw, err := codec.EncodeInput(hooks.Input{
		Event:     axon.Event("PreToolUse"),
		SessionID: "00000000-0000-4000-8000-000000000000",
		Cwd:       "/tmp/axon",
		ToolName:  "Bash",
		ToolInput: map[string]any{"command": "echo hello"},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(string(raw))
}
```

```json
{"cwd":"/tmp/axon","hook_event_name":"PreToolUse","session_id":"00000000-0000-4000-8000-000000000000","tool_input":{"command":"echo hello"},"tool_name":"Bash"}
```

`Resolve` and `hooks.For` return a second boolean rather than an error: an
unknown host, or one with no codec registered, is a lookup miss.

## What you get

- `Resolve` / `Get` / `Hosts` / `HookedHosts`: identity lookup over the
  17 hosts in `spec/`.
- `Events` / `NativeEvents`: the canonical catalog — 32 events, 26 of
  them native to at least one host.
- `hooks.LoadCapabilities` / `Capabilities.Level` / `Recipe`: per-host
  capability maps (native, close, synthesized, unsupported).
- `hooks.For`: `Codec.EncodeInput` / `DecodeInput` / `EncodeDecision` /
  `DecodeDecision` for every hooked host.
- `hooks.ValidateInput` / `ValidateDecision`: check raw bytes against the
  embedded JSON Schemas.
- `invoke.InvocationAdapter`: `Build` turns one normalized `Invocation`
  into a `CommandSpec` plus diagnostics, per adapter under
  `invoke/adapters/<host>` — Go-only, the other two bindings do not carry
  it. See [`invoke/README.md`](invoke/README.md) for the adapter catalog.
- `Detect`: whether a host is installed locally. `Spec`: the embedded
  `spec/` tree as an `fs.FS`.

## When not to use it

axon is spec and protocol only, so it is the wrong dependency when you
want something that runs.

- **You want hooks to actually execute.** axon does not dispatch hooks,
  merge decisions from multiple handlers, manage a socket, or install
  anything into a host's config. For a dispatcher that runs one hook
  across every host, see
  [nerv](https://github.com/hop-top/poly-nerv), which speaks this
  contract.
- **You want session transcripts.** `Detect` and the store roots tell you
  where a host keeps its data; nothing here reads transcript files or
  parses their contents. For finding and reading sessions across hosts,
  see [vein](https://github.com/hop-top/poly-vein); use axon only to
  resolve the paths.
- **You want to test a plugin against these contracts.** axon defines the
  shapes; it does not run your hook and check what it emits.
  [xat](https://github.com/hop-top/xat) is the conformance harness for
  that — one spec, run against each host's own envelope and decision
  shape.
- **You target exactly one host and always will.** That host's own hook
  docs are a shorter path than a canonical shape you would only ever map
  one way.

## Contract

This module is a reader over `spec/`; it defines no data of its own. The
contract every binding follows, including the exact field names and edge
cases (absent vs. zero, the native-event subset, key casing), is
documented once in
[`docs/spec-contract-notes.md`](https://github.com/hop-top/poly-axon/blob/main/docs/spec-contract-notes.md).
Because Go is the reference, a divergence between this module and another
binding is resolved by changing the binding: `make test-parity` runs all
three against the same 71 cases and fails the build when any answer
differs from Go's.

Full API reference: `go doc hop.top/axon` (and `.../hooks`, `.../invoke`),
which works against a local checkout before the module is published. For
the other two bindings and the parity harness, see the
[root README](https://github.com/hop-top/poly-axon/blob/main/README.md).

## License

MIT. See the [`hop-top/poly-axon` LICENSE](https://github.com/hop-top/poly-axon/blob/main/LICENSE).
