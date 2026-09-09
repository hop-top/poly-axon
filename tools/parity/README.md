# Cross-language parity harness

Proves a binding produces the same answer as the Go reference for the same
input read from `spec/`. A binding is trusted when it agrees with Go here,
not when its own unit tests pass.

## Emitter contract (frozen)

An emitter is a program invoked as:

```
<emitter> <case-id>
```

- Reads nothing from stdin.
- Reads `tools/parity/cases.json` (or its own copy of the same case) to
  find `<case-id>`'s `call` and `args`.
- Writes exactly one canonical JSON value to stdout, then exits 0.
- Exit 0 with `{"unsupported": true}` on stdout means "this case cannot be
  answered in this language" — reported as skipped, not a failure, as long
  as at least one other emitter answers the case. The reference language
  (below) is the exception: `unsupported` from it on a case another
  language answered fails the run.
- Any other non-zero exit is a failure. Anything printed to stderr is
  diagnostic only and never parsed.

Canonical JSON: object keys sorted, no insignificant whitespace, UTF-8,
numbers in their shortest round-trip form, one trailing newline.

## The eight operations

| call | args | returns |
|---|---|---|
| `resolve` | `{"name": "<name-or-alias>"}` | `{"found": bool, "name": "<canonical>"}` |
| `hosts` | `{}` | sorted array of canonical names |
| `hooked_hosts` | `{}` | sorted array of canonical names |
| `native_events` | `{}` | sorted array of event names |
| `capability_level` | `{"host": "...", "event": "..."}` | `{"level": "native"\|"close"\|"synthesized"\|"unsupported"\|"", "classified": bool}` |
| `host_event` | `{"host": "...", "event": "..."}` | `{"host_event": "...", "found": bool}` |
| `encode_input` | `{"host": "...", "fixture": "<Event>.input.json"}` | encoded envelope, or `{"error": "<sentinel>"}` |
| `decode_decision` | `{"host": "...", "fixture": "<Event>.<action>.json", "exit": <int>}` | `{"action", "event", "message", "metadata"}`, or `{"error": "<sentinel>"}` |

Not-found values, stated once here because they are easy to get wrong by
guessing: a `resolve` that finds nothing returns `name: ""`, never `null`.
A `host_event` that finds nothing returns `host_event: ""`, never `null`.
A `capability_level` on an event the capability file does not classify at
all returns `level: ""` (not one of the four enum values) with
`classified: false`. `decode_decision`'s `message` is `""`, and its
`metadata` is `{}` (never omitted or `null`), whenever the decoded
Decision carries none. Empty string, empty object — never `null`,
anywhere in this contract.

`capability_level`'s four legal `level` values are exactly `native`,
`close`, `synthesized`, `unsupported` (plus the not-found `""` above). No
other file defines this vocabulary; an implementer needs none beyond this
README.

`decode_decision` sets `Decision.Event` from the fixture's `<Event>` before
returning, matching the reference conformance test. It only decodes; it
never re-encodes and compares.

`fixture` need not exist on disk. `encode_input` then parses `<Event>`
from the name and builds a minimal synthetic input
(`{event, session_id: "s", cwd: "/tmp"}`) — the `ErrUnsupportedEvent` path
for a host with no fixture at all for that event. `decode_decision` may
instead carry a `raw` string in `args`, decoded literally in place of a
fixture file (still needing `fixture`'s `<Event>` prefix to set
`Decision.Event`) — the three no-fixture fallbacks: empty stdout, `{}`,
unrecognized JSON.

Sentinel names in `{"error": "..."}` are contract names, not language
types: `ErrUnknownHost`, `ErrUnsupportedEvent`, `ErrUnsupportedAction`,
`ErrSchema`, mapped from each emitter's own errors (Go via `errors.Is`).

## The reference language

`REFERENCE` in `parity.py` names the authoritative language — `go`. Every
other emitter's output is compared against the reference's, so a
divergence always names a binding disagreeing with the reference rather
than a majority to adjudicate. The role is fixed by that name, not by
position in `LANGUAGES`; reordering the table changes nothing.

The harness refuses to run without the reference rather than letting a
binding stand in for it. `REFERENCE` naming a language absent from
`LANGUAGES`, a reference that fails to build, and a reference returning
`{"unsupported": true}` for a case another language answered are all
setup errors (exit 2). A case *no* language answers is still a skip.

## Runner exit codes (`parity.py`)

- `0` — every case agrees across every emitter that answered it.
- `1` — a divergence: prints the case id, the languages, and a value diff.
- `2` — a usage/setup error: an emitter missing, not executable,
  `cases.json` malformed (including duplicate ids), or the reference
  language unavailable for a case another language answered.

## Where the emitters live

The orchestrator, `cases.json` and `gencases.py` stay at repo root under
`tools/parity/`. An emitter that must be compiled against its own
language's package lives inside that language's directory instead:

| Language | Emitter |
|---|---|
| Go (reference) | `go/tools/parity/main.go` |
| TypeScript | `tools/parity/emitters/ts/main.mjs` |
| Python | `tools/parity/emitters/py/main.py` |

The Go emitter imports `hop.top/axon`, so it has to sit inside the module
that provides those packages — outside it, `go build` finds no `go.mod`.

Only the *build* moves with the language. Every emitter is **run** with the
repo root as its working directory, because the frozen contract above has
each one read `tools/parity/cases.json` by that relative path.

## Adding a language

Add an emitter implementing the shape above, reading the same `cases.json`,
and one `Language` entry to the `LANGUAGES` list at the top of `parity.py`:

- `name` — the label in the report.
- `build` — an optional argv template for a compiled language. `{bin}` is
  substituted with an absolute path this run's build must produce an
  executable at. `None` for an interpreted emitter.
- `run` — an argv template for one case. `{bin}` becomes the path built
  above; with no build step this is the literal run command.
- `build_dir` — the directory the `build` argv runs in, relative to the
  repo root. `None` (the default) means the repo root. Go sets `"go"` so
  its `go build` resolves the module's `go.mod`. This shifts the build
  only; `run` is always invoked from the repo root.
- `unset_env` — environment variables stripped from the build's
  environment. Go strips `GOROOT`, because a stale value inherited from
  the environment makes a `mise`-managed toolchain fail to resolve its own
  standard library. Default empty.

`build_emitters()` iterates `LANGUAGES`; `REFERENCE` is the only place
`parity.py` names a specific language, and a new language is never the
reference. Every case must be answered or declared unsupported — no
partial registration.

## Adding a case

Add one object to `cases.json`: `{"id", "call", "args"}`. Ids stay sorted
and unique; `parity.py` fails the run if either rule breaks. Every
registered emitter must then answer it or return `{"unsupported": true}`.
Fixture-driven cases are generated by `gencases.py`, which walks
`spec/fixtures/`; re-run it rather than hand-editing those entries, and
commit the regenerated `cases.json`.
