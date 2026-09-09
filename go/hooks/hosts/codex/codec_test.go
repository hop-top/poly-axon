package codex

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
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "spec", "fixtures", "hosts", "codex", name)) //nolint:gosec // test fixture path, name is a literal from call sites in this file
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEncodeInputPreToolUseValidates(t *testing.T) {
	c := New()
	in := hooks.Input{Event: axon.EventPreToolUse, SessionID: "s1", Cwd: "/w",
		ToolName: "shell", ToolInput: map[string]any{"command": "echo hi"}}
	raw, err := c.EncodeInput(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := hooks.ValidateInput(axon.HostCodex, axon.EventPreToolUse, raw); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["hook_event_name"] != "PreToolUse" || m["tool_name"] != "shell" {
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

func TestDecodeDecisionDeny(t *testing.T) {
	c := New()
	d, err := c.DecodeDecision(fixture(t, "PreToolUse.block.json"), 0)
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
	h, _ := axon.Get(axon.HostCodex)
	if code != h.ExitCodes.Block {
		t.Fatalf("exit = %d, want %d from host.yaml", code, h.ExitCodes.Block)
	}
	if err := hooks.ValidateDecision(axon.HostCodex, axon.EventPreToolUse, hooks.ActionBlock, raw); err != nil {
		t.Fatal(err)
	}
}

func TestUnsupportedEventIsError(t *testing.T) {
	_, err := New().EncodeInput(hooks.Input{Event: axon.EventPermissionRequest})
	if !errors.Is(err, hooks.ErrUnsupportedEvent) {
		t.Fatalf("got %v, want ErrUnsupportedEvent", err)
	}
}

func TestEmptyStdoutFallsBackToExitCode(t *testing.T) {
	c := New()
	h, _ := axon.Get(axon.HostCodex)
	d, err := c.DecodeDecision([]byte(""), h.ExitCodes.Block)
	if err != nil || d.Action != hooks.ActionBlock {
		t.Fatalf("exit %d -> %+v, %v; want block", h.ExitCodes.Block, d, err)
	}
	d, err = c.DecodeDecision([]byte(""), h.ExitCodes.Allow)
	if err != nil || d.Action != hooks.ActionAllow {
		t.Fatalf("exit %d -> %+v, %v; want allow", h.ExitCodes.Allow, d, err)
	}
}

func equalJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// TestDecodeDecisionUnrecognizedJSONFallsBackToExit pins the same
// fail-open gate claude's codec has: stdout that parses as JSON but
// carries none of codex's known decision keys (bare `{}`, or JSON with
// only unrelated fields) must fall back to actionForExit(exit), exactly
// like empty stdout, rather than defaulting to allow regardless of exit
// code.
func TestDecodeDecisionUnrecognizedJSONFallsBackToExit(t *testing.T) {
	c := New()
	h, _ := axon.Get(axon.HostCodex)
	cases := []struct {
		name   string
		stdout string
		exit   int
		want   hooks.Action
	}{
		{"empty object, block exit", `{}`, h.ExitCodes.Block, hooks.ActionBlock},
		{"empty object, allow exit", `{}`, h.ExitCodes.Allow, hooks.ActionAllow},
		{"unrelated field, block exit", `{"unrelated":1}`, h.ExitCodes.Block, hooks.ActionBlock},
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
// disagree.
func TestDecodeDecisionRecognizedShapeWinsOverExit(t *testing.T) {
	c := New()
	h, _ := axon.Get(axon.HostCodex)
	// top-level decision:"block" is a recognized shape; the allow exit
	// code must not override it.
	d, err := c.DecodeDecision([]byte(`{"decision":"block","reason":"no"}`), h.ExitCodes.Allow)
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != hooks.ActionBlock {
		t.Fatalf("action = %v, want block (recognized shape wins over exit code)", d.Action)
	}
}

func TestEncodeDecisionPostToolUseBlock(t *testing.T) {
	c := New()
	raw, code, err := c.EncodeDecision(hooks.Decision{Event: axon.EventPostToolUse, Action: hooks.ActionBlock, Message: "post-condition failed"})
	if err != nil {
		t.Fatal(err)
	}
	h, _ := axon.Get(axon.HostCodex)
	if code != h.ExitCodes.Block {
		t.Fatalf("exit = %d, want %d from host.yaml", code, h.ExitCodes.Block)
	}
	if err := hooks.ValidateDecision(axon.HostCodex, axon.EventPostToolUse, hooks.ActionBlock, raw); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["decision"] != "block" || m["reason"] != "post-condition failed" {
		t.Fatalf("decision = %v", m)
	}
}

// TestEncodeDecisionSessionStartAllow: a contextless allow (no Metadata)
// emits the bare `{}` envelope, never a shape carrying empty context
// keys. This is the fallback encodeSessionStartContext must produce
// when Metadata has nothing recognized in it.
func TestEncodeDecisionSessionStartAllow(t *testing.T) {
	c := New()
	raw, code, err := c.EncodeDecision(hooks.Decision{Event: axon.EventSessionStart, Action: hooks.ActionAllow})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if string(raw) != "{}" {
		t.Fatalf("contextless SessionStart allow = %s, want {}", raw)
	}
	if err := hooks.ValidateDecision(axon.HostCodex, axon.EventSessionStart, hooks.ActionAllow, raw); err != nil {
		t.Fatal(err)
	}
}

// TestSessionStartAllowContextRoundTrip: the fixture carries context
// injection (additionalContext, continue). Decode must populate
// Decision.Metadata with both, and re-encoding that Decision must
// reproduce the fixture byte-for-byte (JSON-value-equal), not just
// validate.
func TestSessionStartAllowContextRoundTrip(t *testing.T) {
	c := New()
	raw := fixture(t, "SessionStart.allow.json")
	d, err := c.DecodeDecision(raw, 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != hooks.ActionAllow {
		t.Fatalf("Action = %q, want allow", d.Action)
	}
	if d.Event != axon.EventSessionStart {
		t.Fatalf("Event = %q, want SessionStart", d.Event)
	}
	want := map[string]any{
		"additional_context": "session initialized",
		"continue":           true,
	}
	if !equalJSON(d.Metadata, want) {
		t.Fatalf("Metadata = %#v, want %#v", d.Metadata, want)
	}
	d.Event = axon.EventSessionStart
	again, _, err := c.EncodeDecision(d)
	if err != nil {
		t.Fatal(err)
	}
	var a, b map[string]any
	_ = json.Unmarshal(raw, &a)
	_ = json.Unmarshal(again, &b)
	if !equalJSON(a, b) {
		t.Fatalf("round trip not byte-equal:\nfixture: %s\nencoded: %s", raw, again)
	}
	if err := hooks.ValidateDecision(axon.HostCodex, axon.EventSessionStart, hooks.ActionAllow, again); err != nil {
		t.Fatal(err)
	}
}

func TestEncodeDecisionUnsupportedEvent(t *testing.T) {
	c := New()
	_, _, err := c.EncodeDecision(hooks.Decision{Event: axon.EventNotification, Action: hooks.ActionBlock})
	if err == nil {
		t.Fatal("want error for unsupported event")
	}
	if !errors.Is(err, hooks.ErrUnsupportedEvent) {
		t.Fatalf("err = %v, want wrapping ErrUnsupportedEvent", err)
	}
}
