# Capturing hook envelopes

Thirty host/event pairs make `axon fixture` refuse at exit 1 today. This
document is how that number comes down.

## Why a capture, and not a schema

A `capabilities.yaml` row says a host raises an event. It does not say what
the host puts on the wire for it. The repo keeps those two facts apart on
purpose — see [`spec-contract-notes.md` §4](spec-contract-notes.md) — and
`go/hooks/conformance_test.go` states the consequence:

> Fixtures are what prove a codec round-trips the shapes the host actually
> sends; a native event with no fixture is an untested wire path.

So a schema written from the capability row alone tests axon against its
own guess. The only thing that settles a wire shape is running the host and
recording what arrives. `axon capture` is the machinery for doing that
safely and repeatably; the recording itself is yours to make, on your
machine, against your own sessions.

## What the tool does and does not do

| Does | Does not |
| --- | --- |
| Prints the config to add to a host | Write to any host settings file |
| Records envelopes to a directory you name | Write anything under `spec/` |
| Normalizes a recording into fixture shape | Decide a recording is correct |
| Reports what is still missing | Invent a schema or a fixture |

`axon capture handle` refuses any `--dir` that is, or sits under, a
directory named `spec` — the committed tree, the generated `go/spec/`
mirror, and anything added under either later. Promoting a capture to a
fixture is a copy you make after reading the file.

## The loop

### 1. See what is missing

```
axon capture status
```

53 host/event pairs are listed — every `native` and `close` row of the four
hosts with a codec. 22 have a committed fixture; 31 do not. Add `--missing`
for only the gaps, a host name to narrow it, or `--format json` to script
against it.

Thirty of those 31 are also refused by `axon fixture`, because they have no
input schema. The odd one out is `gemini Stop`, which *has* a schema —
gemini maps canonical `Stop` onto host event `SessionEnd` as a close row,
so it shares `SessionEnd`'s shape — but has no fixture of its own.

### 2. Check what the handler will answer

Before installing anything into a live CLI, read what it will say back:

```
axon capture plan claude
```

Every row shows the stdout and exit code the handler emits for that event,
and which of two derivations produced it. Every row exits 0. See
[Why the handler cannot block](#why-the-handler-cannot-block) for how those
responses are derived and what checks them.

### 3. Install the handler

```
axon capture install claude --dir ~/axon-captures
```

This prints. It changes nothing. The output names the settings files from
that host's own `hook_config_paths` and gives you a body to add to one of
them. By default it subscribes only to the events with no fixture yet; pass
`--all` to subscribe to everything.

Add the block by hand, to the file you choose. Take it out when you are
done.

### 4. Use the host normally

Every subscribed event that fires is recorded to
`<dir>/<host>/<Event>.input.json`. Re-running the same event is safe: the
handler reports `unchanged` when the shape matches what it already has, and
`content CHANGED` when it does not — which is the signal that the first
recording missed a variant and both are worth looking at.

### 5. Review, then promote

```
axon capture status --dir ~/axon-captures
```

Captured pairs now read `captured`, with the file to read. Open it. Check
that nothing machine-specific survived, that the keys are what the host
really sent, and that the shape makes sense for the event. Then:

1. Copy it to `spec/fixtures/hosts/<host>/<Event>.input.json`.
2. Write `spec/hosts/<host>/hooks/<Event>.input.schema.json` from what the
   fixture actually contains — `required` for the keys the host always
   sends, `additionalProperties: false`, and the discriminator pinned with
   `const` to the **host-side** event name (gemini's `PostToolUse` schema
   pins `"AfterTool"`, not `"PostToolUse"`).
3. Remove the event from `fixtureDeferred` in
   `go/hooks/conformance_test.go`. Leaving it there now fails, by design:
   the check reports "is in fixtureDeferred but the fixture exists; drop the
   deferral".
4. `make generate` to refresh the `go/spec/` mirror, then `make check`.

`axon fixture <host> <event>` stops refusing the pair at that point, which
is the visible signal that it landed.

## Normalization

A recording is rewritten into the shape the committed fixtures already
have, so promoting one is a copy rather than a cleanup:

- Two-space indent, one top-level key per line, trailing newline.
- Nested objects inline with padded braces: `{ "command": "ls" }`.
- Nested key order preserved as the host sent it. `tool_response` reads
  `{ "stdout": ..., "stderr": ... }` in the committed fixtures, and
  alphabetizing would reverse it.
- Top-level key order from the event's input schema `properties` order
  where a schema exists, and from the host codec's own field order where
  one does not — which is the case for every pair being captured.
- Volatile values replaced with the placeholders every committed fixture
  already uses:

  | Key | Placeholder |
  | --- | --- |
  | `session_id` | `00000000-0000-4000-8000-000000000000` |
  | `cwd` | `/tmp/xat` |
  | `transcript_path` | `/tmp/xat/transcript.jsonl` |
  | `path` | `/tmp/xat/<basename>` |

  Only at the top level. A session id inside a `tool_input` is payload, and
  rewriting it would edit the evidence.
- Keys no schema names are kept. An unrecognized key is usually the reason
  the capture was worth making.

These are not conventions chosen here. `TestNormalizeRoundTripsCommittedFixtures`
feeds all 22 committed input fixtures back through normalization and
requires byte-identical output, so the rules are the tree's rules and a
drift is a test failure. `TestLeadOrderMatchesCommittedFixtures` proves the
same for the no-schema path a real capture takes.

## Why the handler cannot block

A hook handler's stdout and exit code are a decision. Get them wrong on a
blocking event and the host stops: no tool call, no prompt, no turn. So the
handler never composes a response itself. For each event it asks that
host's own codec to encode `Decision{Action: allow}`, and takes what comes
back:

- **The codec returns a shape.** That shape and its exit code are the
  answer. `claude` and `codex` answer `{}`, `gemini` answers
  `{"decision":"allow"}`, `opencode` answers `{"action":"allow"}`.
- **The codec returns `ErrUnsupportedEvent`.** The host's hook contract has
  no decision channel for that event at all — true for 21 of claude's 26
  native events, since Claude only reads a decision back from `PreToolUse`,
  `PermissionRequest`, `PostToolUse` and `SessionStart`. The handler then
  writes nothing and exits with the host's own `exit_codes.allow` from
  `host.yaml`. Every registered codec's `DecodeDecision` reads empty stdout
  at that code as `allow`.

Three tests hold this down, in `go/capture/passthrough_test.go`:

- every host/event pair's response is fed back through that host's
  `DecodeDecision` and must come back `ActionAllow`;
- every pair for which the spec ships an `<Event>.allow.schema.json` has
  its response checked by `hooks.ValidateDecision`;
- all 15 blocking host/event pairs are covered, and none may exit with a
  host's block code.

`go/cmd/capture_test.go` adds the adversarial half: for all four hosts, an
empty stdin, a non-JSON body, a truncated object, an envelope with no
discriminator and one naming an unknown event must each still exit with the
host's allow code and write an allow-decodable response. That test exists
because an earlier draft surfaced an unplaceable envelope as a usage error,
and cobra exits 2 for those — which is claude's *block* code. A handler
installed only to watch would have denied every tool call whose envelope it
failed to parse.

OpenCode is the exception to the shape of all this: its hooks are
in-process JS callbacks, not subprocesses, and a callback blocks by
throwing. The generated plugin therefore wraps everything in `try`/`catch`,
swallows spawn and stdin errors, and contains no `throw` in any executable
line — checked by `TestOpencodeSnippetNeverThrows`.

## Events that are hard to trigger

Some events will not fire just because you used the CLI for an hour. Rough
tiers, and what to do:

**Ordinary use will get these.** `SessionStart`, `SessionEnd`, `Stop`,
`UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `FileChanged`,
`CwdChanged`, `InstructionsLoaded`, `TaskCreated`, `TaskCompleted`. Work
normally for a session or two with the handler installed.

**Need a deliberate action.** `PreCompact` and `PostCompact` (fill the
context window, or invoke the host's compaction command directly);
`SubagentStart` / `SubagentStop` (ask for work that dispatches a subagent);
`PermissionRequest` and `PermissionDenied` (run a tool the host is not
pre-approved for, then deny it); `ConfigChange` (edit a settings file mid
session); `WorktreeCreate` / `WorktreeRemove` (run the git worktree command
the capability row's pattern matches); `Elicitation` /
`ElicitationResult` (call an MCP tool that prompts).

**Hard, and worth being honest about.** `TeammateIdle`, `StopFailure`,
`PostToolUseFailure`, `Notification`. These fire on a failure, a timeout or
a multi-agent condition you cannot reliably stage. Do not fake them, and do
not write a schema for one from the sibling event that *does* fire —
`PostToolUseFailure` is not `PostToolUse` with an error key until an
envelope proves it is.

Two things are legitimate here, and nothing else is:

1. **Leave the handler installed and wait.** These events fire eventually
   during real work. That is what the harness is for: the cost of leaving
   it in place is one subprocess per event, and it never blocks.
2. **Leave the entry in `fixtureDeferred`.** An entry there is a promise
   that the shape is unverified, and it is the honest state until an
   envelope arrives. The conformance suite stays green and no one is misled.

A pair that stays deferred for a long time is not a failure of this
process. A pair whose schema was written from a guess is.

## The open question: gemini UserPromptSubmit

`gemini UserPromptSubmit` is not like the other 29. It is a `close` row,
not a native one:

```yaml
close:
  - { host_event: BeforeModel, event: UserPromptSubmit, note: "fires per model call, not per user prompt" }
```

Every other pair maps onto a host event that some sibling canonical event
also uses, so a shape can at least be cross-read: `gemini Stop` shares
`SessionEnd`'s captured envelope, which is why it has a schema without
having a fixture. `BeforeModel` has no sibling. No canonical event maps to
it natively, so nothing in the tree records what it carries.

The capture answers two questions the repo cannot currently answer:

1. **Does `BeforeModel` carry the prompt at all?** If it does not, the close
   row's mapping to `UserPromptSubmit` is a claim about an envelope with no
   prompt in it, and that is worth knowing before a schema exists.
2. **If it does, is it the user's text or the assembled model input?** The
   note says it fires per model call. A continuation turn, a tool-result
   turn and a compaction all call the model without the user typing
   anything, so the field may hold a rendered prompt containing system
   instructions and history rather than what the user wrote.

The way to settle it: install the gemini handler, type one distinctive
short prompt, let the turn run tools, and read
`<dir>/gemini/UserPromptSubmit.input.json`. If the recorded value is the
sentence you typed, it is the user's text. If it is longer, or you see more
than one recording for a single typed prompt, it is the model input and the
close row's note is doing real work. Either answer belongs in the schema's
`description` when one is written.

## Commands

| Command | What it does |
| --- | --- |
| `axon capture status [host]` | What is captured, what is missing, what to do next |
| `axon capture status --missing` | Only the gaps |
| `axon capture plan <host>` | The response the handler emits per event, and its derivation |
| `axon capture install <host>` | Prints the config to add by hand |
| `axon capture install <host> --all` | Subscribe to every event, not only the uncaptured ones |
| `axon capture handle <host>` | The handler itself; a host runs this |

`--dir` sets where captures land, on any of them. It defaults to
`.axon-captures` in the working directory. Any path with a `spec` segment
is refused, so a capture cannot reach the committed tree by the back door.
