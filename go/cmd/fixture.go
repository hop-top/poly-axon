package cmd

import (
	"fmt"
	"io/fs"

	"github.com/spf13/cobra"
	"hop.top/axon"
	"hop.top/axon/hooks"
)

// hostsWithTranscriptPath names the hosts whose hook envelope actually
// carries a transcript_path key. It is deliberately an allowlist rather
// than a default-on field: transcript_path is a per-host wire field, not a
// canonical Input one, and every host input schema sets
// additionalProperties:false, so emitting it for a host that does not send
// it produces a fixture the host's own schema rejects.
//
// The three hosts below are the ones whose committed golden envelopes carry
// the key (spec/fixtures/hosts/{claude,codex,gemini}/*.input.json — 15
// files, all with transcript_path). OpenCode is absent because none of its
// 11 fixtures has the key, its input schemas do not list it, and its codec
// (go/hooks/hosts/opencode/codec.go) has no transcript concept at all:
// OpenCode hooks are in-process JS/TS plugin callbacks receiving a session
// id, not a subprocess handed a transcript file to read.
var hostsWithTranscriptPath = map[string]bool{
	axon.HostClaude: true,
	axon.HostCodex:  true,
	axon.HostGemini: true,
}

// eventExtra names the host-specific, event-specific keys a host's input
// schema requires that the canonical Input does not carry as a field, so
// they can only travel through Extra. Keyed by host then canonical event.
//
// Both entries below are OpenCode plugin-callback payload fields whose
// required-ness and shape come straight from the host's own schema and
// committed golden envelope:
//   - FileChanged needs "path" (spec/hosts/opencode/hooks/FileChanged.input.schema.json
//     required; spec/fixtures/hosts/opencode/FileChanged.input.json carries a path string)
//   - TaskCompleted needs "todos" (same two files for that event; the
//     golden envelope carries an empty array, the close row's own note
//     being that the canonical event is the status=="completed" filter
//     over that list)
//
// Without these the fixture omits a required property and its own host
// schema rejects it.
var eventExtra = map[string]map[axon.Event]map[string]any{
	axon.HostOpencode: {
		axon.EventFileChanged:   {"path": "/tmp/axon/file.txt"},
		axon.EventTaskCompleted: {"todos": []any{}},
	},
}

// defaultFixtureInput returns the default hooks.Input used by `axon
// fixture` for a host/event pair: a fixed session id, cwd, a Bash tool
// call, a prompt, and — for hosts that send it — a transcript path carried
// in Extra, since it is host-specific rather than a canonical Input field.
//
// ToolResponse is populated because a PostToolUse envelope always reports
// the result of a completed tool call: every host's PostToolUse input
// schema lists tool_response (OpenCode: output) in its required set, and
// every committed PostToolUse fixture carries a real object there. A nil
// ToolResponse marshals to JSON null and is rejected by those schemas, so
// the fixture must supply a plausible response rather than an absent one.
func defaultFixtureInput(host string, ev axon.Event) hooks.Input {
	in := hooks.Input{
		Event:        ev,
		SessionID:    "00000000-0000-4000-8000-000000000000",
		Cwd:          "/tmp/axon",
		ToolName:     "Bash",
		ToolInput:    map[string]any{"command": "echo hello"},
		ToolResponse: map[string]any{"stdout": "hello", "stderr": ""},
		Prompt:       "test prompt",
		Extra:        map[string]any{},
	}
	if hostsWithTranscriptPath[host] {
		in.Extra["transcript_path"] = "/tmp/axon/transcript.jsonl"
	}
	for k, v := range eventExtra[host][ev] {
		in.Extra[k] = v
	}
	return in
}

// inputSchemaPath returns the path, within the spec tree, of the input
// schema that `axon validate input <host> <event>` applies. Schemas are
// keyed by CANONICAL event name even when the envelope's own discriminator
// holds the host-side name (spec/hosts/gemini/hooks/PostToolUse.input.schema.json
// pins hook_event_name to gemini's "AfterTool"), per docs/spec-contract-notes.md §4.
func inputSchemaPath(host string, ev axon.Event) string {
	return fmt.Sprintf("hosts/%s/hooks/%s.input.schema.json", host, ev)
}

// errNoInputSchema reports a host/event pair that axon can encode but has
// no input schema to encode against.
//
// A capabilities.yaml row says the host raises the event; it does not say
// anyone has captured what the host puts on the wire for it. The repo
// treats those as two separate facts on purpose — hook schemas and
// fixtures ship behind capability data, gated on filesystem presence and
// never on a declaration (docs/spec-contract-notes.md §4; the
// fixtureDeferred allowlist in go/hooks/conformance_test.go). Emitting a
// fixture anyway means printing an envelope whose shape nothing has
// verified, which then fails `axon validate input` — so the pair is
// refused, loudly, naming the schema that would settle it.
func errNoInputSchema(host string, ev axon.Event) error {
	return fmt.Errorf(
		"%w: %s/%s: no input schema at spec/%s; the capability row says %s raises %s but no envelope for it has been captured yet, so axon has no verified shape to emit",
		hooks.ErrSchema, host, ev, inputSchemaPath(host, ev), host, ev)
}

func fixtureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fixture <host> <event>",
		Short: "Print a default hook Input encoded for a host",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := resolveHost(args[0])
			if err != nil {
				return err
			}
			codec, err := codecFor(h.Name)
			if err != nil {
				return err
			}
			ev := axon.Event(args[1])
			// Refuse before encoding: a fixture whose own host schema is
			// absent cannot be validated, and printing it would hand the
			// caller an unverifiable envelope with a zero exit status.
			if _, err := fs.Stat(axon.Spec(), inputSchemaPath(h.Name, ev)); err != nil {
				return errNoInputSchema(h.Name, ev)
			}
			raw, err := codec.EncodeInput(defaultFixtureInput(h.Name, ev))
			if err != nil {
				return fmt.Errorf("%w", err)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(raw))
			return nil
		},
	}
}

func init() {
	rootCmd.AddCommand(fixtureCmd())
}
