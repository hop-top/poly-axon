# @hop-top/axon

Hooking into an AI-assistant CLI means learning that host's own wire
format: its event names, its JSON keys, its exit-code convention, and
which events it never reports. Claude Code, Gemini CLI, Codex, and
OpenCode disagree on all four. This package gives you one canonical event
and decision shape plus the codecs that translate to and from each host's
native format, read directly from the shared `spec/` tree. It is the
TypeScript binding for `hop.top/axon`, kept in parity with the Go
reference implementation.

> This repository is a read-only language mirror, republished from the
> `hop-top/poly-axon` monorepo on each release. Open issues and pull
> requests in [`hop-top/poly-axon`](https://github.com/hop-top/poly-axon).

## Install

```sh
pnpm add @hop-top/axon
```

Requirements: Node.js >= 22.

## Usage

Resolve a host by its canonical name or a published alias, then encode a
canonical hook input into that host's native wire format:

```ts
import { resolve, codecFor } from "@hop-top/axon";

const host = resolve("claude-code"); // alias for "claude"
const codec = host && codecFor(host.name);
if (!host || !codec) throw new Error("unknown host or no codec registered");

const raw = codec.encodeInput({
  event: "PreToolUse",
  sessionId: "00000000-0000-4000-8000-000000000000",
  cwd: "/tmp/axon",
  toolName: "Bash",
  toolInput: { command: "echo hello" },
});
console.log(JSON.stringify(raw));
```

```json
{"hook_event_name":"PreToolUse","session_id":"00000000-0000-4000-8000-000000000000","cwd":"/tmp/axon","tool_name":"Bash","tool_input":{"command":"echo hello"}}
```

Both `resolve` and `codecFor` return `undefined` for an unknown host
rather than throwing, so the check above is what a strict TypeScript
project needs to narrow them.

Importing from the package root also registers every built-in hook codec
as a side effect, so `codecFor` resolves without a separate import.

## What you get

- `resolve` / `get` / `hosts` / `hookedHosts`: identity lookup over the
  17 hosts in `spec/`.
- `events` / `nativeEvents` / `effectiveOrigin`: the canonical event
  catalog.
- `loadCapabilities` / `level` / `recipe` / `hostEvent`: per-host
  capability maps (native, close, synthesized, unsupported).
- `codecFor`: `Codec.encodeInput` / `decodeInput` / `encodeDecision` /
  `decodeDecision` for every hooked host.
- `validate`: check raw bytes against the embedded JSON Schemas.
- `specRoot` / `readSpecFile` / `listSpecDir`: direct access to the
  bundled `spec/` tree.

## When not to use it

axon is spec and protocol only, so it is the wrong dependency when you
want something that runs.

- **You want hooks to actually execute.** axon does not dispatch hooks,
  merge decisions from multiple handlers, manage a socket, or install
  anything into a host's config. For a dispatcher that runs one hook
  across every host, see
  [nerv](https://github.com/hop-top/poly-nerv), which speaks this
  contract.
- **You need `invoke` — building a host's native argv.** That surface is
  Go-only; this package covers identity, events, capabilities, and hook
  codecs. Use [`hop.top/axon`](https://github.com/hop-top/axon) if you
  need to launch host CLIs.
- **You want session transcripts.** The spec tells you where a host keeps
  its data; nothing here reads transcript files or parses their contents.
  See [vein](https://github.com/hop-top/poly-vein) for finding and reading
  sessions across hosts.
- **You want to test a plugin against these contracts.** axon defines the
  shapes; it does not run your hook and check what it emits.
  [xat](https://github.com/hop-top/xat) is the conformance harness for
  that — one spec, run against each host's own envelope and decision
  shape.
- **You target exactly one host and always will.** That host's own hook
  docs are a shorter path than a canonical shape you would only ever map
  one way.

## Contract

This package is a reader over `spec/`; it defines no data of its own.
The contract every binding follows, including the exact field names and
the Go-shaped edge cases (absent vs. zero, the native-event subset, key
casing), is documented once in
[`docs/spec-contract-notes.md`](https://github.com/hop-top/poly-axon/blob/main/docs/spec-contract-notes.md).
For the full picture, including the Go reference implementation and the
cross-language parity harness, see the
[root README](https://github.com/hop-top/poly-axon/blob/main/README.md).

## License

MIT. See the [`hop-top/poly-axon` LICENSE](https://github.com/hop-top/poly-axon/blob/main/LICENSE).
