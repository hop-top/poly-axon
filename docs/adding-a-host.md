# Adding a host

Six steps, in order. Each step's test failure disappears once that step is
done; run `go test ./...` from `go/` after any step to see the next failure
in line. The spec files you edit stay at the repo root under `spec/`; the Go
code and tests that read them live in `go/`.

1. **Create `spec/hosts/<name>/host.yaml`.** Minimum fields: `name`,
   `status`, `binaries`. A hooked host also needs `envelope_discriminator`
   and `hook_config_paths`. Until this file exists, `TestDirectoryNameMatchesHostName`
   in `go/registry_test.go` fails with:

   ```
   <name>: open hosts/<name>/host.yaml: file does not exist
   ```

2. **Add `spec/hosts/<name>/capabilities.yaml`** classifying every
   `origin: native` event from `spec/events.yaml` (26 today) as exactly one
   of `native`, `close`, `synthesized`, or `unsupported`. Until every event
   is classified exactly once, `TestEveryNativeEventClassifiedOnce` in
   `go/hooks/capabilities_test.go` fails with:

   ```
   <name>: event <Event> classified 0 times, want exactly 1
   ```

3. **Add the hook schemas**: `spec/hosts/<name>/hooks/<Event>.input.schema.json`
   for every event the host hooks, and `<Event>.<action>.schema.json` for
   every action (`allow`, `warn`, `block`, `rewrite`) the host's
   `capabilities.yaml` and `host.yaml` `exit_codes` say it can emit. A
   missing or unparseable schema makes `hooks.ValidateInput` /
   `ValidateDecision` return an error wrapping `hooks.ErrSchema`.

4. **Add fixtures** under `spec/fixtures/hosts/<name>/`: one
   `<Event>.input.json` per hooked event, one `<Event>.<action>.json` per
   emitted decision. A host with hooks and zero fixtures fails
   `TestFixturesRoundTripAndValidate` in `go/hooks/conformance_test.go` with:

   ```
   <name>: no fixtures under spec/fixtures/hosts/<name>
   ```

   A fixture is a **recording**, never a hand-written example. A
   capability row says the host raises an event; only an envelope
   captured from the running host says what it puts on the wire, and a
   schema written from the row alone tests axon against its own guess.
   `axon capture` records envelopes from a live host and normalizes them
   into fixture shape — see
   [`capturing-envelopes.md`](capturing-envelopes.md) for the loop, and
   for what to do about events that are hard to trigger on purpose. Until
   a shape is recorded, name the event in `fixtureDeferred` at the bottom
   of `go/hooks/conformance_test.go` rather than writing a fixture from
   the schema; `TestEveryNativeEventHasInputFixture` fails otherwise with:

   ```
   <name>: native event <Event> has no spec/fixtures/hosts/<name>/<Event>.input.json and is not in fixtureDeferred
   ```

5. **Write the codec.** Add `go/hooks/hosts/<name>/codec.go` with a `New()`
   constructor implementing `hooks.Codec`, and register it with one line
   in `go/hooks/hosts/all.go` (`hooks.Register(<name>.New())` inside that
   file's `init`). Until the codec is registered, `TestEveryHookedHostHasCodec`
   in `go/hooks/conformance_test.go` fails with:

   ```
   spec/hosts/<name> has hooks: true but no codec is registered
   ```

   New hosts with a hook surface but no codec yet (the way `aider`,
   `goose`, `vibe`, and `openhands` ship today) are named in the
   `codecDeferred` map at the top of `go/hooks/conformance_test.go` so the
   suite stays green while the codec is pending. Removing the host's name
   from `codecDeferred` is the "codec landed" step; do it in the same
   change that adds `codec.go`, or the conformance test starts failing for
   the wrong reason (a deferred host that now also lacks a codec entry).

6. **Regenerate and check**: run `make generate && make check` from the
   repo root. `make generate` runs `go -C go generate ./...`, which
   regenerates `go/events_gen.go`, `go/hosts_gen.go`, `go/invoke/README.md`,
   and the generated `go/spec/` mirror of the root `spec/` tree you just
   edited — that mirror is what `//go:embed` compiles in, so a spec change
   is not picked up until it is regenerated. `make check` then runs the full
   gate: lint, the Go tests, the link checker, the generate drift check, the
   parity harness, and the TypeScript and Python suites (`check: lint test
   links generate-check test-parity test-ts test-py`).

7. **Read a parity failure correctly.** No extra command is needed —
   step 6's `make check` already runs `test-parity`, `test-ts`, and
   `test-py` — but a host is not done just because Go passes: `spec/` is
   read by the TypeScript and Python bindings too. A `test-parity`
   failure means the languages disagree on what the new host's spec files
   say, and the fix is in whichever language's reader is wrong, never in
   the harness itself. If the new host adds a capability or fixture shape
   no existing case exercises, add a case first (see "Adding a case" in
   [`tools/parity/README.md`](../tools/parity/README.md)) so the
   disagreement is caught by a case rather than by hand.

See [`capturing-envelopes.md`](capturing-envelopes.md) for how a fixture
is recorded rather than written, [`spec/README.md`](../spec/README.md) for
the file shapes and the versioning rule, and
[`go/hooks/conformance_test.go`](../go/hooks/conformance_test.go) for the
full list of checks a host must pass.
