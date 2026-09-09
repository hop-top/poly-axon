# spec/ contract notes for language-neutral readers

You are writing a TypeScript or Python reader for `hop.top/axon`'s `spec/`
tree. You have this document, the schemas under `spec/`, and the data files
under `spec/` — no Go source. Every rule below cites the file and line that
proves it, or a command whose output proves it. Where the Go reference
implementation is named, it is named so you can independently confirm the
claim, not because you need to read it.

Two path conventions, because this repo keeps the contract and the reference
implementation in separate trees: `spec/...` paths are relative to the
canonical spec tree at the repo root, and every shell command below is run
from the repo root. Go source paths are relative to the Go module in `go/`,
and are written with that prefix (`go/host.go`, `go/hooks/...`). The Go
module embeds a generated mirror of the spec tree at `go/spec/`; it is byte
-identical to root `spec/` and gated by `make generate-check`, so a rule
proved against one holds for the other.

Counts as of this audit: 17 hosts, 8 `capabilities.yaml`, 11 `invoke.yaml`,
32 events in `events.yaml`, 42 hook schemas under `hosts/*/hooks/`, 42
fixtures under `fixtures/hosts/`, 5 `*.schema.json` at the spec root.

## 1. Key casing

`grep -rn '^\s*[A-Z][A-Za-z_]*:' spec/ --include='*.yaml'` returns 69 lines,
every one of them an event name used as a map key under a `synthesized:`
block in a `capabilities.yaml` (e.g. `spec/hosts/aider/capabilities.yaml:10:
SessionStart:`). This is correct, not a casing bug — see §7. No other YAML
key in `spec/` starts with an uppercase letter (verified: the same grep
restricted to `synthesized:` blocks returns exactly 69 matches, matching the
unrestricted count).

`grep -rno '"[a-z][a-zA-Z]*[A-Z][a-zA-Z]*"\s*:' spec/*.schema.json` (camelCase
JSON property names in the five root schemas) returns only JSON Schema's own
keywords (`additionalProperties`, `uniqueItems`, `minItems`) — never a domain
field. All domain-level YAML and JSON keys in files axon itself defines
(`host.yaml`, `capabilities.yaml`, `invoke.yaml`, `events.yaml`, and all five
`*.schema.json`) are snake_case. `spec/hosts/*/invoke.yaml` was fixed to
snake_case in the commit immediately before this audit (`4457b0e fix(spec):
snake_case keys in invoke.yaml and its schema`); this audit found no
survivors.

**Do not extend this rule to hook input/decision schemas or fixtures.**
`spec/hosts/*/hooks/*.schema.json` and `spec/fixtures/hosts/*/*.json`
describe each host's own wire format verbatim: Claude and Codex use
`hookSpecificOutput`, `permissionDecision`, `hookEventName`, `updatedInput`
(camelCase, matching Claude Code's actual hook protocol — see
`spec/hosts/claude/hooks/PreToolUse.block.schema.json`); OpenCode and Gemini
use `session_id`, `tool_name` (snake_case, matching their own protocols).
Never normalize these — they are the wire vocabulary of the host CLI, not
axon's own schema surface.

## 2. Go-shaped semantics: absence vs. zero

This is the section most likely to produce a silently-wrong binding. Go
structs give every field a zero value; YAML absence and Go's zero value are
indistinguishable once decoded. A binding decoding into plain
maps/dicts (the normal TS/Python approach) does not have this problem by
construction — but only if you check *presence*, not value, everywhere the
Go reader relies on a zero value. The known cases:

### 2a. `exit_codes` absent means "no exit-code contract"

`spec/host.schema.json:19-22` makes `exit_codes` an optional object; when
present it requires `allow` and `block` (both `integer`, which includes 0).
Go's `Host.ExitCodes` (`go/host.go:11-18`) is a plain (non-pointer) struct, so
a host with no `exit_codes:` key decodes to `ExitCodes{Allow:0, Warn:0,
Block:0, Rewrite:0, Error:0}` — indistinguishable, in Go, from a host whose
real allow-exit-code is 0 and whose block code is also 0.

Verify which hosts have no exit-code contract at all:

```
$ grep -L "^exit_codes:" spec/hosts/*/host.yaml
spec/hosts/amp/host.yaml
spec/hosts/antigravity/host.yaml
spec/hosts/copilot/host.yaml
spec/hosts/crush/host.yaml
spec/hosts/cursor-agent/host.yaml
spec/hosts/kimi/host.yaml
spec/hosts/opencode/host.yaml
spec/hosts/qwen/host.yaml
spec/hosts/tabnine/host.yaml
spec/hosts/windsurf/host.yaml
```

`opencode` is the case that matters: it is `hooks: true`
(`spec/hosts/opencode/host.yaml`) with `envelope_discriminator: type`, but
no `exit_codes:` key — because OpenCode hooks run in-process and signal
block by throwing, not by process exit code. A binding must check **key
presence** in the parsed document (`"exit_codes" in doc`, `doc.exit_codes !==
undefined`), never `=== 0` or `== null`, to distinguish "no exit-code
contract" from "contract with a real 0". `go/hooks/conformance_test.go`'s
`exitFor` helper (lines 126-140) reads `h.ExitCodes.Allow` etc. directly and
is only correct today because every codec that consults it belongs to a
host that *does* declare `exit_codes:`; a binding for an
`exit_codes`-absent host must not port that pattern.

### 2b. `technique` and `cost` absent in a synthesized row

`spec/capabilities.schema.json`'s `recipe` definition (lines 24-34) has
`"required": []`: every property, including `technique` and `cost`, is
optional. `spec/README.md` states the rule directly: "`technique` and `cost`
are optional because only nerv's codex adapter states them. Their absence
means nerv does not classify that row; nothing is inferred from it." Do not
default a missing `technique` to some "unknown" sentinel that participates
in matching logic, and do not treat a missing `cost` as `"low"` or any other
enum member — leave it absent/undefined and propagate that absence.

### 2c. `origin` absent means "native" — and every event declares it anyway

`spec/events.yaml:12-13`: "Every event declares `origin: native | derived |
extension`. Default = native (omitted = native)." Confirmed twice over: the
catalog holds 32 events and 32 explicit `origin:` lines, so the documented
default is never actually exercised by the data.

```console
$ grep -c '^  - name:' spec/events.yaml
32
$ grep '^    origin:' spec/events.yaml | sort | uniq -c
   2     origin: derived
   4     origin: extension
  26     origin: native
```

Origin is therefore the whole test for the host-contract scope, and Go's
`NativeEvents()` (`go/events.go`) applies nothing else:

```go
func NativeEvents() []Event {
	var out []Event
	for _, e := range Events() {
		if e.EffectiveOrigin() == "native" {
			out = append(out, e.Name)
		}
	}
	return out
}
```

`EventInfo.EffectiveOrigin()` keeps the documented absent-means-native
fallback so a future catalog entry that omits the key is classified as the
header promises, but no entry relies on it today.

A binding reproduces the same 26-event host-contract set Go gets
(`go/hooks/capabilities_test.go`'s `TestClaudeIsAllNative` pins
`len(caps.Native) == 26`) by filtering on origin alone. The 6 excluded events
are the 4 `extension` (native to one CLI, not all) and 2 `derived`
(synthesized from `PreToolUse`, never emitted) entries.

### 2d. Other zero-value-shaped fields, checked and cleared

- `Host.Hooks` (`hooks: true|false`, `omitempty` in Go) — absence and
  `false` are semantically identical here (a host with no hook surface); no
  finding, but note that `hooks: true` does **not** imply a `hooks/`
  directory exists on disk — see §4.
- `Host.Aliases`, `Host.ConfigFilePatterns`, `Host.HookConfigPaths` — Go
  slices default to `nil`/empty on absence; the schema types them as plain
  arrays with no `minItems`, so empty array and absent key both decode
  sensibly as "no items". No special handling needed beyond normal
  empty-array treatment.
- `EventInfo.Blocking` (`bool`, required in the schema, no `omitempty` in
  Go) — always present in every event in `events.yaml`; not a zero-value
  risk.
- `Derivation` (`go/events.go:9`) is a Go **pointer** (`*Derivation`), so Go
  itself distinguishes "no derivation" from a present-but-empty one; the
  schema requires both `from` and `when` when `derivation:` is present
  (`spec/events.schema.json`'s `derivation` definition, `required: ["from",
  "when"]`). A binding gets this for free by checking key presence.
- `hosts.Capabilities.HostVersionRange` (`omitempty`) — cosmetic optional
  string; no semantic absence trap.

## 3. Enums, complete, per schema

Copy these directly; do not re-derive them by reading Go constants.

**`spec/version.schema.json`** — `version`: string matching
`^[0-9]+\.[0-9]+\.[0-9]+$`. No enum.

**`spec/host.schema.json`**
- `status`: `active`, `retired`. (Every host in this spec tree today is
  `active`; `grep -h "^status:" spec/hosts/*/host.yaml | sort -u` returns
  only `status: active` — `retired` is declared but unexercised.)
- `project_key_strategy`: `slash-to-dash`, `sha1`, `sha256`, `md5`,
  `basename-alias`, `embedded`, `none`. (Unexercised in the current tree —
  no host sets this key today; still validate against the full enum.)
- `name` / `aliases[]` pattern: `^[a-z][a-z0-9-]*$`.
- Required top-level: `name`, `status`, `binaries`.
- Conditional requirement (`if`/`then`, see §5): when `hooks: true`, also
  required: `envelope_discriminator`, `hook_config_paths`.
- `exit_codes` object, when present, requires `allow` and `block`
  (`integer`); `warn`, `rewrite`, `error` optional integers.

**`spec/events.schema.json`**
- `direction`: `inbound`, `outbound`.
- `origin`: `native`, `extension`, `derived` (absence = native, see §2c).
- Required per event: `name`, `category`, `description`, `direction`,
  `blocking`.
- `category` is a free-form string, not an enum, but the file's own index
  comment at the end of `spec/events.yaml` enumerates every value actually
  in use: `session`, `prompt`, `tool`, `permission`, `notification`,
  `agent`, `task`, `context`, `file`, `worktree`, `mcp`, `memory`. Confirmed
  by `grep "^    category:" spec/events.yaml | sort -u` — exactly these 12
  values, nothing else. Category plays no part in the host-contract scope;
  every category participates and `origin` alone decides (§2c).
- `derivation` (only on `origin: derived` events): requires `from` (string)
  and `when` (arbitrary object — the schema does not constrain its shape
  beyond `type: object`; match-clause keys like `tool_in` / `path_matches`
  are a documentation convention in `spec/events.yaml`'s header comment,
  not schema-enforced).
- `payload[].type` is a free-form string (`string`, `object`, `array`,
  `boolean`, etc. by convention — not schema-enforced as an enum).
- `payload[].format`: free-form string; no event in the catalog uses it
  today (confirmed by `grep -c "        format:" spec/events.yaml`).

**`spec/capabilities.schema.json`**
- `recipe.technique`: `matcher`, `daemon`, `status_diff`, `binary_wrap`,
  `mcp_proxy`. In use today: `matcher`, `daemon`, `status_diff`,
  `binary_wrap` (confirmed by `grep -h "technique:"
  spec/hosts/*/capabilities.yaml | sort -u`); `mcp_proxy` is declared but
  unused.
- `recipe.cost`: `low`, `medium`, `high`. All three are in use.
- Required top-level: `host`, `native`. `close`, `synthesized`,
  `unsupported` all optional.
- `pair` (native/synthesized-map keys aside): requires `host_event`,
  `event`.
- `pair_with_note` (used for `close:` entries only): requires `host_event`,
  `event`, `note`.
- `recipe`: `required: []` — every field optional (§2b).

**`spec/invoke.schema.json`**
- `option_mapping.support` / `tool_capability.support`: `native`, `shim`,
  `unsupported`, `dangerous`. All four in use
  (`grep -h "support:" spec/hosts/*/invoke.yaml | sort -u`).
- `tool_capability.permission`: `read`, `write`, `exec`, `network`,
  `browser`, `task`.
- `tool_capability.transcript`: `native`, `partial`, `unavailable`.
- Required on `option_mapping`: `universal`, `support`.
- Required on `tool_capability`: `universal`, `support`, `permission`,
  `transcript`, `controllable`.
- **This file is generated, not authored.** `go/invoke/parity_test.go`'s
  `TestParityIsUpToDate` regenerates every `spec/hosts/<host>/invoke.yaml`
  from the Go `invoke` package's adapters (`go/internal/gen/invoke/emit`) and
  fails the Go build if the committed file differs. A binding reads this
  file; it never writes it, and a binding project must not treat it as
  something to keep in sync by hand.

None of these five schemas use `oneOf`, `anyOf`, `patternProperties`,
`propertyNames`, or `dependencies` — confirmed by grepping all five plus
all 42 hook schemas for those keywords, zero matches outside `host.schema.json`'s
one `if`/`then` (§5).

## 4. Path and packaging assumptions

The Go module reads this tree through `//go:embed spec` in `go/spec.go`,
which embeds the generated `go/spec/` mirror (a `go:embed` directive cannot
traverse up to the root tree), exposed as an `fs.FS` rooted at `spec/`. A
binding reads the same tree from disk (or from its own embedded/bundled
copy); all paths below are relative to that `spec/` root, matching what
`axon.Spec()` returns.

Canonical path shapes, and the exact rule for which schema validates which
file — this is `go/internal/spectest/spectest.go`'s `schemaFor`, reproduced
here in full so a binding does not have to open that file:

```
version.yaml                                  → version.schema.json
events.yaml                                   → events.schema.json
hosts/<name>/host.yaml                        → host.schema.json
hosts/<name>/capabilities.yaml                → capabilities.schema.json
hosts/<name>/invoke.yaml                      → invoke.schema.json
```

`spectest.ValidateAll` walks every `.yaml` file under `spec/` and treats any
file matching none of the five rules above as an error ("no schema for this
path") — there is no fallback or wildcard schema. A binding's own
walk-and-validate should apply the same five rules and treat any unmatched
`.yaml` file the same way: a hard error, not a skip.

Hook schemas and fixtures are **not** `.yaml` and are not in this table;
their shape is positional, not walked by `schemaFor`:

```
hosts/<name>/hooks/<Event>.<kind>.schema.json   — hook input/decision schema
fixtures/hosts/<name>/<Event>.<kind>.json       — golden envelope/decision
```

`<kind>` is `input` for a hook's incoming envelope, or one of the four
`hooks.Action` values (`allow`, `warn`, `block`, `rewrite`) for an outgoing
decision. The mapping from a fixture's file name to the schema that
validates it is: split the base name on `.` — `<Event>.input.json` validates
against `hosts/<name>/hooks/<Event>.input.schema.json`;
`<Event>.<action>.json` validates against
`hosts/<name>/hooks/<Event>.<action>.schema.json`. This exact split-and-map
is what `go/hooks/conformance_test.go`'s `TestFixturesRoundTripAndValidate`
does (lines 59-110, the full switch) and what a binding's fixture-driven
test must reproduce.

**Directory presence is not implied by `host.yaml`'s `hooks:` field.**
`hooks: true` means "this host has a hook surface and therefore
`capabilities.yaml`", per the field's own schema description
(`spec/host.schema.json:24`) — it does **not** mean a `hooks/` schema
directory or fixtures exist for that host yet. Verified:

```
$ for h in aider goose vibe openhands; do
    grep "^hooks:" spec/hosts/$h/host.yaml
    test -d spec/hosts/$h/hooks && echo "  hooks/ exists" || echo "  hooks/ MISSING"
    test -d spec/fixtures/hosts/$h && echo "  fixtures exist" || echo "  fixtures MISSING"
  done
aider:   hooks: true
  hooks/ MISSING
  fixtures MISSING
goose:   hooks: true
  hooks/ MISSING
  fixtures MISSING
vibe:    hooks: true
  hooks/ MISSING
  fixtures MISSING
openhands: hooks: true
  hooks/ MISSING
  fixtures MISSING
```

These four ship a `capabilities.yaml` (the event-mapping data) ahead of a
codec; Go's own conformance suite names this explicitly
(`go/hooks/conformance_test.go`'s `codecDeferred` map: `"aider": true, "goose":
true, "vibe": true, "openhands": true`). A binding must gate "does this host
have hook schemas/fixtures to read" on the actual presence of the `hooks/`
directory (or an equivalent host allowlist it maintains), never on
`host.yaml`'s `hooks:` boolean alone — that boolean answers a capabilities
question, not a filesystem-layout question.

Only 4 of the 8 hooked-capability hosts currently have a `hooks/` directory:
`claude`, `codex`, `gemini`, `opencode`. Each has fixtures in
`fixtures/hosts/<name>/`. Only these four hosts have registered Go codecs
today (`hooks.Registered()`); this is the set a binding's hook-codec layer
needs to cover to match current parity, not all 17 hosts and not all 8
hook-capable hosts.

## 5. JSON Schema dialect

All 47 schema files (5 at `spec/` root + 42 under `hosts/*/hooks/`) declare
`"$schema": "http://json-schema.org/draft-07/schema#"` — confirmed by
extracting that field from every schema file in the tree and finding a
single distinct value. Use a draft-07-capable validator:

- TypeScript: **ajv**, with the draft-07 metaschema (ajv's default in v6;
  for ajv v8+, pass `{schemaId: "$id"}` is not needed here since none of
  these schemas set `$id` — but do add `ajv-formats` if you want `format:
  rfc3339`-style hints to actually check anything, since draft-07 `format`
  is advisory-only by spec and event payload `format: rfc3339` fields rely
  on this).
- Python: **jsonschema**, `Draft7Validator`.

Keywords in use across the tree, confirmed by grep: `$ref` + `definitions`
(only in `events.schema.json`, `capabilities.schema.json`,
`invoke.schema.json` — all self-referencing, no external `$ref`s, no
`$id`), `const` (host discriminator fields in hook input schemas, and
`host.schema.json`'s `if`), `enum`, `pattern`, `uniqueItems`, `minItems`,
`additionalProperties`. **Not** in use anywhere: `oneOf`, `anyOf`, `allOf`,
`not`, `patternProperties`, `propertyNames`, `dependencies`, `$id`.

**`if`/`then` appears exactly once**, in `spec/host.schema.json`:

```json
"if": {
  "properties": { "hooks": { "const": true } },
  "required": ["hooks"]
},
"then": {
  "required": ["envelope_discriminator", "hook_config_paths"]
}
```

Check this explicitly against your validator: `if`'s own `required: ["hooks"]`
means the conditional only applies when `hooks` is present in the document
*and* equal to `true`; a host with `hooks` absent, or `hooks: false`, does
not trigger `then` and is not required to carry
`envelope_discriminator`/`hook_config_paths`. Both ajv and Python
`jsonschema`'s `Draft7Validator` implement `if`/`then` per the draft-07
spec identically here — there is no known engine-specific divergence for
this exact shape (no `else` branch, no nested conditionals) — but it is the
one construct worth a dedicated test in your binding's schema test suite,
because it is the one place validity depends on a boolean's presence *and*
its value together rather than each property independently.

## 6. Fixture and round-trip contract

`go/hooks/conformance_test.go`'s `TestFixturesRoundTripAndValidate` (lines
48-113) is the reference for this section, but it enforces **two different
guarantees** depending on fixture kind — do not assume both apply to both
kinds. Of the 42 fixtures, 22 are `<Event>.input.json` (envelope fixtures)
and 20 are `<Event>.<action>.json` (decision fixtures).

**Input fixtures (22 of them): decode-then-encode is value-equal to the
file, and this is enforced today.** The input branch
(`conformance_test.go:62-77`) decodes the fixture, re-encodes it, and calls
`sameJSON(raw, again)` (line 73) — an explicit deep-equality check between
the original bytes and the round-tripped bytes, order- and
whitespace-insensitive. A binding's input codec must satisfy the same
check: parse the fixture as your canonical `Input` type, re-serialize it,
and compare the two JSON documents by value. Then validate the re-encoded
output against the schema for that `<Event>.input` path (§4's mapping) —
a codec that decodes a fixture correctly but encodes a shape the schema
itself would reject is still broken.

**Decision fixtures (20 of them): decode-then-re-encode-then-validate,
with no equality check against the file.** The decision branch
(`conformance_test.go:79-108`) decodes the fixture, asserts the decoded
`Action` matches the fixture's own file name (line 87 — `<Event>.block.json`
must decode to `Action == "block"`, and so on; the four canonical actions,
spelled exactly this way on the wire and in the file name, are `allow`,
`warn`, `block`, `rewrite`, per `go/hooks/input.go:8-12`), sets `Decision.Event`
from the file name (line 97, needed because a blank `Event` falls back to a
codec's default tool-gate shape), re-encodes, and validates the re-encoded
bytes against the schema for that `<Event>.<action>` path. **It never
compares the re-encoded bytes back to the original fixture file.** A
decision fixture can therefore, in principle, decode-and-validate correctly
while still differing byte-for-byte from what the codec would produce for
that same `Decision` — the conformance suite as it stands would not catch
that gap.

That said, §8 documents a case (`hookEventName` on four claude/codex
decision fixtures) where exactly that kind of gap was found and fixed by
checking byte-equality by hand, because a fixture that under-represents
what a codec actually emits teaches every binding the wrong shape even
though today's suite passes around it. The rule for a binding to hold
itself to, stricter than what `TestFixturesRoundTripAndValidate` currently
enforces: **a decision fixture is byte-equal (post-JSON-canonicalization)
to what the reference Go codec emits for the `Decision` its file name
names.** Building your own decision-fixture test to this stricter bar,
rather than only what the Go suite checks today, is how you catch a
codec/fixture drift before it ships. See §8 for the full worked example and
the byte-equal comparison.

Two more checks are worth carrying over even though they are not literally
about round-tripping a single fixture:

1. **Native rows win over close rows on decode, always.** A host event
   listed under both `native:` and `close:` in `capabilities.yaml` must
   decode to the **native** row's canonical event, never the close row's.
   Close-only rows (a host event no native row claims) still decode fine —
   they're unambiguous. This is `go/hooks/schema.go`'s `DecodeMap` doc comment
   and `TestDecodeMapNativeWinsOverClose`
   (`go/hooks/capabilities_test.go:141-160`); the concrete motivating case is
   Gemini, which lists host event `SessionEnd` twice — once
   `native → SessionEnd`, once `close → Stop` — and only the native row is
   what Gemini actually emits when it fires that event.
2. **Two rows in the same section (`native:` or `close:`) sharing a host
   event is an error, not a last-one-wins.** If your capability loader ever
   finds a duplicate `host_event` within one section, refuse to build a
   decode map rather than silently keeping whichever row parsed last.

A fixture directory with zero fixtures for a registered codec is itself a
failure (`conformance_test.go:53-56`) — every codec you register must have
at least one fixture backing it. Conversely, not every native event needs a
fixture yet: Go's `fixtureDeferred` map (`conformance_test.go`, `var
fixtureDeferred`) names specific native events per host that ship without a
captured golden envelope because no real payload has been recorded from the
host yet. A binding does not need to reproduce that exact deferral list (it
is a Go-test-only allowlist, not spec data), but should not assume "every
native event has a fixture" as an invariant — check what's actually on disk
under `fixtures/hosts/<name>/`.

### 6a. DecodeDecision's exit-code fallback must gate on a *recognized* shape, not just non-empty JSON

This is a Go-behavior contract, not `spec/` data, but it is a fail-open
trap every binding's decision decoder must reproduce, so it is recorded
here rather than left to be independently rediscovered.

Every host with an `exit_codes` contract (§2a — claude, gemini, codex;
opencode is the declared exception, see below) has a `DecodeDecision`
whose stdout-empty branch falls back to the exit code
(`actionForExit`/`actionForExit`-equivalent). The trap is what happens
when stdout is **not** empty but also does not contain any of that host's
known decision keys (a bare `{}`, or JSON with only unrelated fields):
the fallback to the exit code must still apply. It is not enough to gate
on "stdout parsed as JSON without error" — a hook that exits with the
block code and prints `{}` must still decode to `block`, not silently to
`allow` because `{}` "parsed fine" and left every field at its zero
value. Claude's codec (`go/hooks/hosts/claude/codec.go`) and codex's codec
(`go/hooks/hosts/codex/codec.go`) implement this via a `decodeKnownShape`
helper returning `(Decision, recognized bool)`; `DecodeDecision` falls
back to `actionForExit(exit)` whenever `recognized` is `false`, exactly
as it does for empty stdout. Gemini's codec
(`go/hooks/hosts/gemini/codec.go`) was missing this gate — its
`DecodeDecision` had no `recognized` tracking at all and returned
`ActionAllow` (the zero-value default) for any unrecognized shape
regardless of exit code — and has been fixed to the same pattern. A
binding's decoder for any host with an exit-code contract needs the same
two-outcome gate: recognized-shape-present (trust the shape) vs.
recognized-shape-absent (trust the exit code), never "did it parse."

**Codex was already correct**, contrary to what an earlier draft of the
task that produced this fix assumed ("gemini and codex both lack the
gate"). Verified directly against a throwaway probe calling
`DecodeDecision` on all four hooked-host codecs with `{}` and with
`{"unrelated":1}`, each at the block exit code, before any code was
changed: claude and codex both already returned `block` (correct);
gemini returned `allow` (the bug); opencode returned `allow` too, but
that is correct for opencode — see next paragraph. Do not assume a
brief's stated defect surface is exactly right; verify per-host before
fixing.

**OpenCode is exempt, by design, not by omission.** `host.schema.json`'s
`hooks:true` path does not require `exit_codes` (§2a), and
`spec/hosts/opencode/host.yaml` has no `exit_codes:` key at all — its
hooks are in-process JS/TS callbacks that signal block by throwing, never
by process exit code (the file's own comment states this; confirmed
again by reading `go/hooks/hosts/opencode/codec.go`, whose `DecodeDecision`
signature ignores its exit argument entirely — `func (c *codec)
DecodeDecision(stdout []byte, _ int) (hooks.Decision, error)` — and whose
only fallback, for empty stdout, is the literal `ActionAllow`, never a
value derived from `exit`). A binding must not add an exit-code fallback
gate to its OpenCode decoder; there is no exit code to fall back to.

The cross-language parity harness (`tools/parity/`) previously only
exercised this fallback gate for claude
(`decode_decision/claude/fallback-{empty-stdout,empty-object,unrecognized-json}`);
gemini and codex now have the same three cases each
(`tools/parity/gencases.py`'s fallback-case loop), so a future regression
of this specific gate in either language's gemini or codex codec fails
`make test-parity`, not just that language's own unit tests.

## 7. Event names are the wire vocabulary, not something to normalize

Canonical event names (`SessionStart`, `PreToolUse`, `WorktreeCreate`, …)
are **PascalCase strings**, and they legitimately appear as PascalCase YAML
map keys wherever a `capabilities.yaml`'s `synthesized:` section names them
(e.g. `spec/hosts/aider/capabilities.yaml:10: SessionStart:`). This is the
source of every "PascalCase key" a naive linter would flag in §1's grep.
Do not lowercase, snake_case, or otherwise rewrite these keys when parsing
— they are values from the fixed 32-event vocabulary in `events.yaml`, used
as map keys purely as a YAML authoring convenience, and your reader's
`Event` type should be a plain string (or string-newtype) carrying the
value through unchanged. Every catalog event is PascalCase, but do not
depend on that: host-side wire names are not (OpenCode raises
`tool.execute.before`), and the catalog is free to admit one.

## 8. What was fixed in this task, and what is backlog

This section is a historical record of the audit that produced this
document. Its Go file paths and line numbers are as they stood at that time,
when the module was at the repo root; those files are now under `go/` (so
`hooks/hosts/gemini/codec.go` below is today `go/hooks/hosts/gemini/codec.go`),
and the line numbers have since moved.

**Fixed** (additions inside `spec/*.schema.json` that tighten without
changing what validates today — verified per file against every fixture
that exercises it, then confirmed by the full test suite):

- Added `"additionalProperties": false` to all 39
  `spec/hosts/*/hooks/*.schema.json` files that lacked it at the top level
  (and to nested `hookSpecificOutput` objects within them, where that
  nested object had no existing `additionalProperties`), matching the
  convention already used in the five root schemas
  (`required` immediately followed by `additionalProperties`, before
  `properties`). Each addition was checked against every fixture file that
  validates against that schema before being applied; the full Go test
  suite (`go test -buildvcs=false -count=1 ./...`) was run after. A first
  pass surfaced four schemas where this addition initially broke
  `TestFixturesRoundTripAndValidate` — see the `hookEventName` item below,
  which corrects the fixtures (the actual gap) rather than leaving the
  schemas loose.
- Corrected `spec/README.md`'s `hosts/<name>/invoke.yaml` bullet, which
  read "option mappings and tool taxonomy, seeded from kit invoke." This
  was true before commit `792dca9` ("refactor(invoke): move invocation
  adapters from kit uxp, generate invoke.yaml per host"), which moved the
  adapters into axon's own `invoke` package. `invoke.yaml` is now generated
  from that in-repo package (`internal/gen/invoke/emit`), and axon
  explicitly forbids importing `hop.top/kit` (`hooks/conformance_test.go`'s
  `TestGoModHasNoKit`, and this task's own hard rules). A binding author
  who trusted the old sentence would go looking for a `kit` dependency that
  does not exist. Reworded to name the actual generator and state plainly
  that the file is read-only from a binding's perspective.

**Fixed, second pass — the fixture was the actual gap:**

- `spec/fixtures/hosts/claude/PreToolUse.block.json`,
  `spec/fixtures/hosts/claude/PreToolUse.rewrite.json`,
  `spec/fixtures/hosts/codex/PreToolUse.block.json`,
  `spec/fixtures/hosts/codex/PreToolUse.rewrite.json`: each was missing
  `hookSpecificOutput.hookEventName`, a key the Claude and Codex codecs
  (`hooks/hosts/claude/codec.go:167-179`, `hooks/hosts/codex/codec.go:170-181`)
  always emit on encode for these two decision kinds — it is how
  `DecodeDecision` later recovers which event a standalone decision payload
  answers (`hooks/hosts/claude/codec.go:274-284`,
  `hooks/hosts/codex/codec.go:240-249`). Added
  `"hookEventName": "PreToolUse"` to all four fixtures, with the value read
  directly off the codec (both codecs hardcode the literal string
  `"PreToolUse"` in every branch of `encodePreToolUseDecision` for these two
  actions — there was nothing to infer). Verified byte-equal to the
  codec's real output with a throwaway probe test (decode each fixture,
  set `Decision.Event`, re-encode, canonicalize both sides through
  `json.Marshal` of the parsed value, compare strings):

  ```
  fixtures/hosts/claude/PreToolUse.block.json
  fixture: {"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"blocked by policy"}}
  codec  : {"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"blocked by policy"}}
  byte-equal: true

  fixtures/hosts/claude/PreToolUse.rewrite.json
  fixture: {"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","updatedInput":{"command":"echo hello --safe"}}}
  codec  : {"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","updatedInput":{"command":"echo hello --safe"}}}
  byte-equal: true

  fixtures/hosts/codex/PreToolUse.block.json    — same shape, byte-equal: true
  fixtures/hosts/codex/PreToolUse.rewrite.json  — same shape, byte-equal: true
  ```

  With the fixtures corrected, `additionalProperties: false` was
  (re-)applied to all four schemas' nested `hookSpecificOutput` objects,
  `hookEventName` was added to `properties` (typed as `{"type": "string",
  "enum": ["PreToolUse"]}`, since both codecs only ever emit this literal
  for these two decision kinds on this event), and to `required` (both
  codecs emit it unconditionally in every branch — it is never optional on
  the wire for these two decision kinds). `go test -buildvcs=false
  ./hooks/...` passes in full, including `TestFixturesRoundTripAndValidate`
  and `TestEncodeDecisionBlockValidates`, the two tests that failed on the
  first pass.

  **The general rule for binding authors**: a decision fixture is
  byte-equal (after JSON-value canonicalization — key order and whitespace
  don't count, values do) to what the reference Go codec emits for the
  `Decision` that fixture's file name names. If your binding's encoder
  produces different bytes than the fixture for the same `Decision` value,
  your encoder is wrong, not the fixture — the fixture is the reference,
  not a loose example. When you find a fixture that looks incomplete
  relative to what a schema permits, treat that as a lead to go read the
  reference codec's actual encode path (as this task did), not a reason to
  loosen the schema around the fixture's gap.

**Fixed, third pass — a real codec gap, not a fixture or schema gap:**

- `spec/fixtures/hosts/{claude,codex}/SessionStart.allow.json` carry
  `hookSpecificOutput.additionalContext`, `systemMessage`, and `continue`
  — Claude's (and Codex's identical) SessionStart context-injection shape,
  the entire reason a SessionStart hook exists. `encodeSessionStartDecision`
  for `ActionAllow` returned the bare `{}` envelope unconditionally, so
  the reference codec itself could not produce the shape its own fixture
  documents. This was a **codec bug**, not a fixture or schema problem —
  the fixture was right.

  Fix: `hooks.Decision.Metadata` (a pre-existing, previously-unused
  `map[string]any` field with no other reader or writer anywhere in the
  module — confirmed by grepping for `.Metadata` across all non-test Go
  source before touching it) now carries three recognized keys,
  `additional_context` (string), `system_message` (string), and
  `continue` (bool). `encodeSessionStartDecision`
  (`hooks/hosts/claude/codec.go`, `hooks/hosts/codex/codec.go`) builds
  Claude/Codex's native shape from whichever of the three are set and
  falls back to `{}` when Metadata is empty or carries none of them;
  `decodeKnownShape` in both codecs now recognizes
  `hookSpecificOutput.additionalContext`, top-level `systemMessage`, and
  top-level `continue`, setting `Event = SessionStart` and populating the
  same three Metadata keys on decode. Both codecs implement this
  independently (matching the module's existing pattern of parallel,
  not shared, per-host codec logic) rather than sharing a helper across
  the `claude`/`codex` packages.

  Added tests: `TestSessionStartAllowContextRoundTrip` (claude and codex)
  decodes the fixture, asserts `Metadata` holds exactly the three keys,
  re-encodes, and asserts the result is JSON-value-equal to the original
  fixture bytes — the byte-equal rule from §6/§8 enforced as an actual
  test, not just a hand-run probe. `TestEncodeDecisionSessionStartAllow`
  was strengthened to assert a contextless allow's raw output is
  literally `"{}"`, not merely schema-valid.

  Mutation-tested: reverted both codec.go files to their pre-fix content
  (via `git show HEAD:<path>`, not `git checkout`, so the working tree's
  other changes were undisturbed), confirmed
  `TestSessionStartAllowContextRoundTrip` fails red on both hosts with
  the original `{}`-only encoder, then restored the fix and confirmed
  green again.

  Full sweep, per the coordinator's request: wrote a throwaway test that
  decodes and re-encodes **all 42 fixtures** (not just the four from the
  second pass, not just the two SessionStart ones) across all four
  registered codecs, comparing each pair by JSON value. Result: **42
  checked, 0 mismatches** — no other fixture documents a shape its
  reference codec cannot produce. The throwaway test was deleted after
  producing this result; it is not part of the committed suite (the two
  named tests above are the permanent regression coverage for this
  specific gap).

**Fixed, fourth pass — gemini's `DecodeDecision` fail-open gate:**

- `hooks/hosts/gemini/codec.go`'s `DecodeDecision` had no "recognized
  shape" concept at all: it parsed stdout into a map and read `m["decision"]`
  directly, so `{}` or any JSON without a `decision` key left `Decision`
  at its `ActionAllow` zero value regardless of the exit code — a hook
  that exited with the block code and printed `{}` was silently read as
  `allow`. This is the same fail-open class §6a's claude/codex
  `decodeKnownShape` gate exists to prevent; gemini was the one codec
  that never got it.

  Verified empirically before fixing anything: a throwaway probe test
  (`internal/probetmp`, deleted after use) called `DecodeDecision` on all
  four registered hosts with `{}` and `{"unrelated":1}`, each at exit 2
  (every hooked host's block code):

  ```
  claude    stdout={}                     exit=2 => Action="block"  err=<nil>
  claude    stdout={"unrelated":1}        exit=2 => Action="block"  err=<nil>
  codex     stdout={}                     exit=2 => Action="block"  err=<nil>
  codex     stdout={"unrelated":1}        exit=2 => Action="block"  err=<nil>
  gemini    stdout={}                     exit=2 => Action="allow"  err=<nil>
  gemini    stdout={"unrelated":1}        exit=2 => Action="allow"  err=<nil>
  opencode  stdout={}                     exit=2 => Action="allow"  err=<nil>
  opencode  stdout={"unrelated":1}        exit=2 => Action="allow"  err=<nil>
  ```

  Confirming: claude and codex were already correct (codex already had
  its own `decodeKnownShape`/`recognized` gate, mirroring claude's —
  contrary to this fix's originating brief, which assumed codex also
  lacked it); gemini was the one real defect; opencode's `allow` is
  correct by design (§6a) since it has no exit-code contract to fall
  back to.

  Fix: added a `decodeKnownShape(m map[string]any) (hooks.Decision,
  bool)` helper to `hooks/hosts/gemini/codec.go`, structured like
  claude's/codex's — `DecodeDecision` now falls back to
  `actionForExit(exit)` when `recognized` is `false`, same as the
  empty-stdout branch. The existing `systemMessage`-based warn/allow fold
  (gemini's own lossy-wire documentation: a plain allow and a warn both
  carry `decision:"allow"` plus `reason`, and only `systemMessage`
  distinguishes them) was preserved byte-for-byte inside the helper —
  it disambiguates an already-recognized `decision:"allow"`/`"deny"`
  shape, so it plays no part in setting `recognized` itself; only
  `m["decision"]` being `"deny"`/`"allow"`, and
  `hookSpecificOutput.tool_input` being present (the rewrite channel),
  set `recognized = true`.

  Added tests to `hooks/hosts/gemini/codec_test.go`:
  `TestDecodeDecisionUnrecognizedJSONFallsBackToExit` (table: `{}` at the
  block exit → block, `{}` at the allow exit → allow, unrelated-field
  JSON at the block exit → block) and
  `TestDecodeDecisionRecognizedShapeWinsOverExit` (a recognized
  `decision:"deny"` shape must win over a disagreeing allow exit code).
  Mirrored equivalent additions onto `hooks/hosts/codex/codec_test.go`
  (`TestDecodeDecisionUnrecognizedJSONFallsBackToExit`,
  `TestDecodeDecisionRecognizedShapeWinsOverExit`) even though codex
  needed no code change, since codex had no test naming this gate
  explicitly by name before (only the narrower
  `TestEmptyStdoutFallsBackToExitCode`).

  Mutation-tested: reverted `hooks/hosts/gemini/codec.go` to its pre-fix
  content via `git show HEAD:<path>` (not `git checkout`), confirmed
  `TestDecodeDecisionUnrecognizedJSONFallsBackToExit` fails red (`{}`
  and unrelated-JSON at the block exit both returned `allow`, want
  `block`), then restored the fix and confirmed green again.

  **Cross-language consequence**: `ts/src/hooks/hosts/gemini.ts`
  deliberately mirrors Go's current per-host behavior, bug included, so
  its `decodeDecision` had the identical gap and its test suite
  (`ts/tests/hooks/gemini.test.ts`) had three tests explicitly pinning
  the old buggy behavior under an "I5 (gemini has no recognized-shape
  gate)" heading. `codex.ts` already had its own `decodeKnownShape`/
  `recognized` gate mirroring Go's (already-correct) codex, so it needed
  no change. Fixed `gemini.ts` with the same `decodeKnownShape` helper
  shape as `codex.ts`'s, and corrected the three pinned assertions in
  `gemini.test.ts`: the `{}`-at-block-exit and
  unrecognized-JSON-at-block-exit cases now assert `"block"` instead of
  `"allow"` (their headings and comments rewritten to say the gate now
  exists and cites the Go fix), and one new test
  (`"a recognized shape still wins over a disagreeing exit code"`) was
  added alongside them — these tests were pinning a bug, so correcting
  the expected value is the right fix, not a weakening.

  `tools/parity/` (65 cases before this task) only exercised this gate
  for claude; adding the fix without extending parity coverage would
  have left the class of bug undetected by the harness for any host but
  claude. `tools/parity/gencases.py`'s hand-authored fallback-case block
  was generalized from a single `claude`-only loop to iterate
  `("claude", "gemini", "codex")`, each contributing the same three
  no-fixture cases (`fallback-empty-stdout`, `fallback-empty-object`,
  `fallback-unrecognized-json`) at that host's own block exit code from
  `HOST_EXIT`. Regenerated `cases.json` (`python3
  tools/parity/gencases.py`) deterministically — a second run produces
  an identical diff — for **71 cases total** (65 + 6 new), no duplicate
  ids (the generator's own dupe check would otherwise exit 1). Before
  fixing `gemini.ts`, `make test-parity` correctly went red on the two
  new gemini cases (`go` answered `block`, `ts` still answered `allow`);
  after the `gemini.ts` fix, `make test-parity` passes with all 71 cases
  agreeing, 0 skipped. opencode was deliberately not given fallback
  cases here: it has no exit-code contract (§6a), so a "fallback to exit
  code" case does not apply to it.

**Not fixed — backlog, out of this task's scope:**

- The remaining ~30 property-level opportunities this audit noticed in
  passing (missing per-field `description`s across all five root schemas;
  `payload_field.type`/`payload[].type` and `category` being free-form
  strings rather than enums even though a closed set is documented in
  comments) are intentionally **not** touched here: none of them are
  one-line, unambiguously-safe additions in the way `additionalProperties`
  was (verified empirically, file by file, against real fixtures), and
  brief step 7 caps this task's fix-now scope at exactly that kind of
  change. Recorded here as a backlog item for whoever owns spec quality
  next, not actioned.

## 9. Gates run

Both commands are run from the **repo root**. `make` enters the module for
you, so there is no need to `cd go` first.

```
$ make test
go -C go test -buildvcs=false ./...
ok  	hop.top/axon
ok  	hop.top/axon/cmd
ok  	hop.top/axon/hooks
ok  	hop.top/axon/hooks/hosts/claude
ok  	hop.top/axon/hooks/hosts/codex
ok  	hop.top/axon/hooks/hosts/gemini
ok  	hop.top/axon/hooks/hosts/opencode
ok  	hop.top/axon/internal/gen/ident
ok  	hop.top/axon/internal/spectest
ok  	hop.top/axon/invoke
ok  	hop.top/axon/invoke/adapters/... (11 adapters)
ok  	hop.top/axon/invoke/shim
(all other packages: no test files)

$ make lint
cd go && mise exec -- golangci-lint run ./...
0 issues.
```

`-buildvcs=false` is not decoration: a bare `go build ./...` inside a git
worktree fails with `error obtaining VCS status: exit status 128`, which is
why every Makefile build target carries the flag. Running the toolchain
directly instead of through `make` means supplying it yourself, from `go/`:

```
$ cd go && go build -buildvcs=false ./...
$ cd go && go test ./...
```
