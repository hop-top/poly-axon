// Package capture records real hook envelopes from host CLIs and
// normalizes them into the shape a committed fixture needs.
//
// The repo refuses to write a fixture from a guess: a capability row says
// a host raises an event, it does not say what the host puts on the wire
// for it (docs/spec-contract-notes.md §4, and the fixtureDeferred
// allowlist in go/hooks/conformance_test.go). This package is the other
// half of that rule — the machinery for getting a real envelope onto disk
// so a fixture can be written from a recording instead of an invention.
//
// Nothing here writes into spec/. A capture lands in a working directory
// the operator names; promoting one to a fixture is a deliberate, reviewed
// copy.
package capture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"hop.top/axon"
)

// Placeholders are the fixed values every committed fixture uses in place
// of a volatile field. They are read off the 22 committed input fixtures
// under spec/fixtures/hosts/, not chosen here: TestNormalizeRoundTripsCommittedFixtures
// re-normalizes each of those files and requires a byte-identical result,
// so changing a value below breaks that test rather than silently
// diverging from the tree.
const (
	// SessionID replaces every host-generated session identifier.
	SessionID = "00000000-0000-4000-8000-000000000000"
	// Cwd replaces the absolute working directory the host reports.
	Cwd = "/tmp/axon"
	// TranscriptPath replaces the absolute transcript file path.
	TranscriptPath = "/tmp/axon/transcript.jsonl"
	// Indent is the two-space indent every committed fixture uses.
	Indent = "  "
)

// volatileString maps a wire key whose value is volatile to the fixed
// placeholder that replaces it. Keys are matched exactly, at the top level
// of the envelope only: a nested object's "path" is payload, not envelope
// metadata, and rewriting it would edit the shape being recorded.
//
// Every entry is a key that appears in a committed fixture carrying a
// placeholder value today:
//   - session_id: all 22 input fixtures
//   - cwd, transcript_path: the 15 claude/codex/gemini fixtures
//     (opencode's plugin-callback envelopes carry neither)
//   - path: spec/fixtures/hosts/opencode/FileChanged.input.json, whose
//     placeholder is Cwd-prefixed rather than Cwd itself
var volatileString = map[string]string{
	"session_id":      SessionID,
	"cwd":             Cwd,
	"transcript_path": TranscriptPath,
}

// cwdPrefixed names top-level keys holding an absolute path *inside* the
// working directory rather than the working directory itself. Their value
// is rewritten to Cwd + "/" + base name, preserving the file name the host
// reported while dropping the machine-specific prefix. FileChanged's
// "path" is the one such key in the committed tree
// (spec/fixtures/hosts/opencode/FileChanged.input.json: "/tmp/axon/file.txt").
var cwdPrefixed = map[string]bool{"path": true}

// dropVolatile names top-level keys whose value is a timestamp or another
// per-run token with no stable placeholder in the committed tree. They are
// left untouched rather than rewritten: no committed fixture carries one,
// so inventing a placeholder here would be exactly the guess this package
// exists to avoid. Recorded values stay visible so a reviewer sees them
// and decides. The set is empty on purpose and documented so the next
// person does not read its absence as an oversight.
var dropVolatile = map[string]bool{}

// leadOrder is the per-host prefix of the key order a normalized envelope
// uses. Keys not named here follow, sorted alphabetically.
//
// Each list is the host codec's own field-assignment order in EncodeInput
// (go/hooks/hosts/<host>/codec.go), with the host-identity keys the codec
// carries through Extra spliced in at the position the committed fixtures
// put them:
//
//   - claude/gemini: discriminator, session_id, cwd, then the canonical
//     Input fields in hooks.Input declaration order (tool_name, tool_input,
//     tool_response, prompt). transcript_path is an Extra key, placed after
//     cwd where all 15 committed fixtures carry it.
//   - codex: same, plus model and turn_id — Extra keys in codex's codec —
//     between transcript_path and the tool fields, per all five committed
//     codex fixtures.
//   - opencode: type, session_id, then opencode's own wire names for the
//     canonical fields (tool, input, output, prompt). It has no cwd or
//     transcript concept at all (its host.yaml omits exit_codes and its
//     codec has no transcript field).
//
// TestLeadOrderMatchesCommittedFixtures pins every list against the
// committed fixtures, so a wrong entry fails rather than producing a
// capture that a reviewer has to hand-reorder.
var leadOrder = map[string][]string{
	axon.HostClaude: {
		"hook_event_name", "session_id", "cwd", "transcript_path",
		"tool_name", "tool_input", "tool_response", "prompt",
	},
	axon.HostCodex: {
		"hook_event_name", "session_id", "cwd", "transcript_path",
		"model", "turn_id",
		"tool_name", "tool_input", "tool_response", "prompt",
	},
	axon.HostGemini: {
		"hook_event_name", "session_id", "cwd", "transcript_path",
		"tool_name", "tool_input", "tool_response", "prompt",
	},
	axon.HostOpencode: {
		"type", "session_id",
		"tool", "input", "output", "prompt",
	},
}

// Normalize turns a raw envelope recorded from a host into the shape a
// committed fixture has: two-space-indented JSON, a deterministic key
// order, volatile values replaced by the placeholders every existing
// fixture uses, and a trailing newline.
//
// Key order comes from the event's own input schema when one exists — the
// schema's "properties" declaration order reproduces all 22 committed
// fixtures exactly — and from leadOrder[host] otherwise, which is the case
// that matters here: a captured envelope for a deferred pair has no schema
// yet, that being why it is being captured.
//
// Normalize never consults the network, the clock, or the environment: the
// same input yields the same bytes on any machine, which is what makes a
// capture reviewable as a diff.
func Normalize(host string, ev axon.Event, raw []byte) ([]byte, error) {
	if _, ok := axon.Get(host); !ok {
		return nil, fmt.Errorf("capture: unknown host %q", host)
	}
	doc, err := decodeObject(raw)
	if err != nil {
		return nil, fmt.Errorf("capture: %s/%s: envelope is not a JSON object: %w", host, ev, err)
	}
	scrubbed := scrub(doc)
	scrubbed.keys = keyOrder(host, ev, scrubbed)
	var buf bytes.Buffer
	if err := writeObject(&buf, scrubbed, ""); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

// scrub replaces the top-level volatile values with their placeholders.
// Nested values are left exactly as recorded: a session id inside a
// tool_input is part of the payload shape being captured, and rewriting it
// would edit the evidence.
func scrub(doc *object) *object {
	out := &object{values: make(map[string]any, len(doc.keys))}
	for _, k := range doc.keys {
		v := doc.get(k)
		switch {
		case dropVolatile[k]:
			out.set(k, v)
		case cwdPrefixed[k]:
			out.set(k, cwdPrefix(v))
		default:
			if ph, ok := volatileString[k]; ok {
				if _, isString := v.(string); isString {
					out.set(k, ph)
					continue
				}
			}
			out.set(k, v)
		}
	}
	return out
}

// cwdPrefix rewrites an absolute path to Cwd + "/" + its base name,
// leaving a non-string or an already-relative value untouched.
func cwdPrefix(v any) any {
	s, ok := v.(string)
	if !ok || s == "" {
		return v
	}
	base := s
	if i := strings.LastIndex(s, "/"); i >= 0 {
		base = s[i+1:]
	}
	if base == "" {
		return v
	}
	return Cwd + "/" + base
}

// keyOrder returns the keys of doc in fixture order: schema-declared order
// when the event has an input schema, else the host's leadOrder prefix,
// with anything unranked sorted alphabetically after.
func keyOrder(host string, ev axon.Event, doc *object) []string {
	rank := schemaOrder(host, ev)
	if rank == nil {
		rank = leadOrder[host]
	}
	idx := make(map[string]int, len(rank))
	for i, k := range rank {
		idx[k] = i
	}
	keys := make([]string, len(doc.keys))
	copy(keys, doc.keys)
	sort.SliceStable(keys, func(i, j int) bool {
		ri, oki := idx[keys[i]]
		rj, okj := idx[keys[j]]
		if oki != okj {
			return oki
		}
		if oki && ri != rj {
			return ri < rj
		}
		return keys[i] < keys[j]
	})
	return keys
}

// schemaOrder reads the "properties" declaration order out of a host
// event's input schema, or returns nil when there is no such schema. The
// declaration order of a JSON object is not preserved by encoding/json's
// map decode, so the schema is scanned with a token decoder instead.
func schemaOrder(host string, ev axon.Event) []string {
	path := fmt.Sprintf("hosts/%s/hooks/%s.input.schema.json", host, ev)
	raw, err := fs.ReadFile(axon.Spec(), path)
	if err != nil {
		return nil
	}
	return propertyOrder(raw)
}

// propertyOrder returns the keys of the top-level "properties" object in
// declaration order.
func propertyOrder(raw []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	// Consume the opening brace of the schema document.
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil
		}
		key, _ := keyTok.(string)
		if key != "properties" {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil
			}
			continue
		}
		return objectKeys(dec)
	}
	return nil
}

// objectKeys reads one JSON object from dec and returns its keys in
// declaration order.
func objectKeys(dec *json.Decoder) []string {
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil
	}
	var out []string
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil
		}
		key, _ := keyTok.(string)
		out = append(out, key)
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil
		}
	}
	return out
}

// writeObject renders doc with the given key order, two-space indentation
// and no HTML escaping, matching the committed fixtures byte for byte.
// Nested values keep their own recorded key order (encoding/json sorts map
// keys), which is what a fixture's nested objects show today.
func writeObject(buf *bytes.Buffer, doc *object, prefix string) error {
	if len(doc.keys) == 0 {
		buf.WriteString("{}")
		return nil
	}
	inner := prefix + Indent
	buf.WriteString("{\n")
	for i, k := range doc.keys {
		buf.WriteString(inner)
		if err := writeJSON(buf, k); err != nil {
			return err
		}
		buf.WriteString(": ")
		if err := writeValue(buf, doc.get(k), inner); err != nil {
			return err
		}
		if i < len(doc.keys)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString(prefix)
	buf.WriteByte('}')
	return nil
}

// writeValue renders one value. Nested objects and arrays are rendered on
// one line when they fit the way the committed fixtures render them
// ({ "command": "ls" }), and expanded otherwise.
func writeValue(buf *bytes.Buffer, v any, prefix string) error {
	switch t := v.(type) {
	case *object:
		return writeNestedObject(buf, t, prefix)
	case []any:
		return writeArray(buf, t, prefix)
	default:
		return writeJSON(buf, v)
	}
}

// writeNestedObject renders a nested object inline with padded braces —
// `{ "command": "ls" }` — which is how every committed fixture renders one.
// An empty object is `{}`. Keys keep the order the host sent them in:
// spec/fixtures/hosts/gemini/PostToolUse.input.json's tool_response reads
// {"stdout": ..., "stderr": ...}, which alphabetizing would reverse.
func writeNestedObject(buf *bytes.Buffer, doc *object, prefix string) error {
	if len(doc.keys) == 0 {
		buf.WriteString("{}")
		return nil
	}
	buf.WriteString("{ ")
	for i, k := range doc.keys {
		if i > 0 {
			buf.WriteString(", ")
		}
		if err := writeJSON(buf, k); err != nil {
			return err
		}
		buf.WriteString(": ")
		if err := writeValue(buf, doc.get(k), prefix); err != nil {
			return err
		}
	}
	buf.WriteString(" }")
	return nil
}

// writeArray renders an array inline: `[]` when empty, `[a, b]` otherwise.
// The one committed fixture array of each kind is empty
// (claude PermissionRequest's permission_suggestions, opencode
// TaskCompleted's todos).
func writeArray(buf *bytes.Buffer, items []any, prefix string) error {
	if len(items) == 0 {
		buf.WriteString("[]")
		return nil
	}
	buf.WriteString("[")
	for i, it := range items {
		if i > 0 {
			buf.WriteString(", ")
		}
		if err := writeValue(buf, it, prefix); err != nil {
			return err
		}
	}
	buf.WriteString("]")
	return nil
}

// writeJSON marshals a scalar with HTML escaping off, so a command
// containing &, < or > is recorded as the host sent it rather than as
// &.
func writeJSON(buf *bytes.Buffer, v any) error {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("capture: encode %v: %w", v, err)
	}
	buf.Write(bytes.TrimRight(b.Bytes(), "\n"))
	return nil
}
