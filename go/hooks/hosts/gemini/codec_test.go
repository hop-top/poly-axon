package gemini

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"hop.top/axon"
	"hop.top/axon/hooks"
)

func fixture(t *testing.T, name string) []byte {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "spec", "fixtures", "hosts", "gemini", name)) //nolint:gosec // test fixture path, name is a literal from call sites in this file
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEncodeInputPreToolUseValidates(t *testing.T) {
	c := New()
	in := hooks.Input{Event: axon.EventPreToolUse, SessionID: "s1", Cwd: "/w",
		ToolName: "Bash", ToolInput: map[string]any{"command": "echo hi"}}
	raw, err := c.EncodeInput(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := hooks.ValidateInput(axon.HostGemini, axon.EventPreToolUse, raw); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	// Gemini's native name for PreToolUse is BeforeTool.
	if m["hook_event_name"] != "BeforeTool" || m["tool_name"] != "Bash" {
		t.Fatalf("envelope = %v", m)
	}
}

func TestInputRoundTrip(t *testing.T) {
	c := New()
	raw := fixture(t, "PreToolUse.input.json")
	in, err := c.DecodeInput(raw)
	if err != nil {
		t.Fatal(err)
	}
	again, err := c.EncodeInput(in)
	if err != nil {
		t.Fatal(err)
	}
	var a, b map[string]any
	_ = json.Unmarshal(raw, &a)
	_ = json.Unmarshal(again, &b)
	if !equalJSON(a, b) {
		t.Fatalf("round trip differs:\n%s\n%s", raw, again)
	}
}

func TestPostToolUseInputRoundTrip(t *testing.T) {
	c := New()
	raw := fixture(t, "PostToolUse.input.json")
	in, err := c.DecodeInput(raw)
	if err != nil {
		t.Fatal(err)
	}
	again, err := c.EncodeInput(in)
	if err != nil {
		t.Fatal(err)
	}
	var a, b map[string]any
	_ = json.Unmarshal(raw, &a)
	_ = json.Unmarshal(again, &b)
	if !equalJSON(a, b) {
		t.Fatalf("round trip differs:\n%s\n%s", raw, again)
	}
}

func TestSessionStartInputRoundTrip(t *testing.T) {
	c := New()
	raw := fixture(t, "SessionStart.input.json")
	in, err := c.DecodeInput(raw)
	if err != nil {
		t.Fatal(err)
	}
	again, err := c.EncodeInput(in)
	if err != nil {
		t.Fatal(err)
	}
	var a, b map[string]any
	_ = json.Unmarshal(raw, &a)
	_ = json.Unmarshal(again, &b)
	if !equalJSON(a, b) {
		t.Fatalf("round trip differs:\n%s\n%s", raw, again)
	}
}

func TestDecodeDecisionDeny(t *testing.T) {
	c := New()
	d, err := c.DecodeDecision(fixture(t, "PreToolUse.block.json"), 2)
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != hooks.ActionBlock || d.Message == "" {
		t.Fatalf("decision = %+v", d)
	}
}

func TestEncodeDecisionBlockValidates(t *testing.T) {
	c := New()
	raw, code, err := c.EncodeDecision(hooks.Decision{Action: hooks.ActionBlock, Message: "no"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 2 {
		t.Fatalf("exit = %d, want 2 from host.yaml", code)
	}
	if err := hooks.ValidateDecision(axon.HostGemini, axon.EventPreToolUse, hooks.ActionBlock, raw); err != nil {
		t.Fatal(err)
	}
}

func TestEncodeDecisionAllowValidates(t *testing.T) {
	c := New()
	raw, code, err := c.EncodeDecision(hooks.Decision{Action: hooks.ActionAllow})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0 from host.yaml", code)
	}
	if err := hooks.ValidateDecision(axon.HostGemini, axon.EventPreToolUse, hooks.ActionAllow, raw); err != nil {
		t.Fatal(err)
	}
}

func TestEncodeDecisionRewriteValidates(t *testing.T) {
	c := New()
	raw, code, err := c.EncodeDecision(hooks.Decision{Action: hooks.ActionRewrite, Rewrite: map[string]any{"command": "echo safe"}})
	if err != nil {
		t.Fatal(err)
	}
	// host.yaml has no rewrite exit code; falls back to allow's.
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (no native rewrite exit code)", code)
	}
	if err := hooks.ValidateDecision(axon.HostGemini, axon.EventPreToolUse, hooks.ActionAllow, raw); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["decision"] != "allow" {
		t.Fatalf("decision = %v, want allow", m["decision"])
	}
	hso, ok := m["hookSpecificOutput"].(map[string]any)
	if !ok {
		t.Fatalf("hookSpecificOutput missing: %v", m)
	}
	if _, ok := hso["tool_input"]; !ok {
		t.Fatalf("hookSpecificOutput.tool_input missing: %v", hso)
	}
}

func TestEncodeDecisionWarnMirrorsNervFold(t *testing.T) {
	c := New()
	raw, code, err := c.EncodeDecision(hooks.Decision{Action: hooks.ActionWarn, Message: "careful"})
	if err != nil {
		t.Fatal(err)
	}
	// nerv's FromDecision never bumps the exit code for Warn (only Block
	// does); gemini's host.yaml has no warn code either, so this exits
	// like allow.
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (nerv only raises exit code for block)", code)
	}
	// warn has no dedicated schema in xat's hook-output; validate against
	// axon's own PreToolUse.warn.schema.json, which mirrors the shape
	// FromDecision actually emits (decision:"allow" + reason + systemMessage).
	if err := hooks.ValidateDecision(axon.HostGemini, axon.EventPreToolUse, hooks.ActionWarn, raw); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["decision"] != "allow" {
		t.Fatalf("decision = %v, want allow (nerv folds warn into allow)", m["decision"])
	}
	if m["reason"] != "careful" || m["systemMessage"] != "careful" {
		t.Fatalf("envelope = %v, want reason and systemMessage both = careful", m)
	}
}

func TestDecodeDecisionWarnFixture(t *testing.T) {
	c := New()
	d, err := c.DecodeDecision(fixture(t, "PreToolUse.warn.json"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != hooks.ActionWarn || d.Message != "consider a safer flag" {
		t.Fatalf("decision = %+v, want ActionWarn with the fixture's message", d)
	}
}

// TestDecodeDecisionUnrecognizedJSONFallsBackToExit pins the fail-open
// fix: stdout that parses as JSON but carries none of Gemini's known
// decision keys (bare `{}`, or JSON with only unrelated fields) must
// fall back to actionForExit(exit), exactly like empty stdout, rather
// than defaulting to allow regardless of exit code. Before this fix
// DecodeDecision had no "recognized" gate at all, so a hook that exited
// with the block code and printed `{}` was silently read as allow.
func TestDecodeDecisionUnrecognizedJSONFallsBackToExit(t *testing.T) {
	c := New()
	cases := []struct {
		name   string
		stdout string
		exit   int
		want   hooks.Action
	}{
		{"empty object, block exit", `{}`, 2, hooks.ActionBlock},
		{"empty object, allow exit", `{}`, 0, hooks.ActionAllow},
		{"unrelated field, block exit", `{"unrelated":1}`, 2, hooks.ActionBlock},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := c.DecodeDecision([]byte(tc.stdout), tc.exit)
			if err != nil {
				t.Fatal(err)
			}
			if d.Action != tc.want {
				t.Fatalf("action = %v, want %v", d.Action, tc.want)
			}
		})
	}
}

// TestDecodeDecisionRecognizedShapeWinsOverExit ensures a recognized
// decision shape is trusted over the exit code even when the two
// disagree: the gate added for the unrecognized-JSON case must not
// start overriding a decision the wire shape actually states.
func TestDecodeDecisionRecognizedShapeWinsOverExit(t *testing.T) {
	c := New()
	// decision:"deny" is a recognized shape; exit 0 (allow's code)
	// must not override it.
	d, err := c.DecodeDecision([]byte(`{"decision":"deny","reason":"no"}`), 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != hooks.ActionBlock {
		t.Fatalf("action = %v, want block (recognized shape wins over exit code)", d.Action)
	}
}

func TestDecodeDecisionAllowWithoutSystemMessageStaysAllow(t *testing.T) {
	c := New()
	// Same "reason" field as warn, but no systemMessage: on the wire this
	// is indistinguishable from a plain allow, so it must decode to
	// allow, not warn (see the comment on DecodeDecision's fold).
	d, err := c.DecodeDecision([]byte(`{"decision":"allow","reason":"fyi"}`), 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != hooks.ActionAllow {
		t.Fatalf("decision = %+v, want ActionAllow (no systemMessage to distinguish warn)", d)
	}
}

func TestEncodeInputUnsupportedEvent(t *testing.T) {
	c := New()
	_, err := c.EncodeInput(hooks.Input{Event: axon.EventPermissionRequest})
	if !errors.Is(err, hooks.ErrUnsupportedEvent) {
		t.Fatalf("err = %v, want ErrUnsupportedEvent", err)
	}
}

func TestSessionStartDecisionRoundTrip(t *testing.T) {
	c := New()
	raw := fixture(t, "SessionStart.allow.json")
	if err := hooks.ValidateDecision(axon.HostGemini, axon.EventSessionStart, hooks.ActionAllow, raw); err != nil {
		t.Fatal(err)
	}
	d, err := c.DecodeDecision(raw, 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != hooks.ActionAllow {
		t.Fatalf("decision = %+v", d)
	}
}

func equalJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// TestGeminiSessionEndDecodesToSessionEnd pins the Native-only decode
// rule. gemini's capability file lists host event SessionEnd twice:
// native -> SessionEnd and close -> Stop. Close rows are lossy encode-side
// approximations and must never win a decode.
func TestGeminiSessionEndDecodesToSessionEnd(t *testing.T) {
	c := New()
	in, err := c.DecodeInput([]byte(`{"hook_event_name":"SessionEnd","session_id":"s","cwd":"/tmp"}`))
	if err != nil {
		t.Fatalf("DecodeInput: %v", err)
	}
	if in.Event != axon.EventSessionEnd {
		t.Fatalf("decoded event = %q, want %q", in.Event, axon.EventSessionEnd)
	}
}
