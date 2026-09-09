package claude

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
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "spec", "fixtures", "hosts", "claude", name)) //nolint:gosec // test fixture path, name is a literal from call sites in this file
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
	if err := hooks.ValidateInput(axon.HostClaude, axon.EventPreToolUse, raw); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["hook_event_name"] != "PreToolUse" || m["tool_name"] != "Bash" {
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
	if code != 2 {
		t.Fatalf("exit = %d, want 2 from host.yaml", code)
	}
	if err := hooks.ValidateDecision(axon.HostClaude, axon.EventPreToolUse, hooks.ActionBlock, raw); err != nil {
		t.Fatal(err)
	}
}

func equalJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

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

func TestEncodeDecisionPermissionRequestBlockValidates(t *testing.T) {
	c := New()
	raw, code, err := c.EncodeDecision(hooks.Decision{
		Event: axon.EventPermissionRequest, Action: hooks.ActionBlock, Message: "no tool access",
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 2 {
		t.Fatalf("exit = %d, want 2 from host.yaml", code)
	}
	if err := hooks.ValidateDecision(axon.HostClaude, axon.EventPermissionRequest, hooks.ActionBlock, raw); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	hso, _ := m["hookSpecificOutput"].(map[string]any)
	dec, _ := hso["decision"].(map[string]any)
	if dec["behavior"] != "deny" || dec["message"] != "no tool access" {
		t.Fatalf("decision = %v", dec)
	}
}

func TestEncodeDecisionPermissionRequestAllowValidates(t *testing.T) {
	c := New()
	raw, code, err := c.EncodeDecision(hooks.Decision{Event: axon.EventPermissionRequest, Action: hooks.ActionAllow})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if err := hooks.ValidateDecision(axon.HostClaude, axon.EventPermissionRequest, hooks.ActionAllow, raw); err != nil {
		t.Fatal(err)
	}
}

func TestEncodeDecisionPermissionRequestRewrite(t *testing.T) {
	c := New()
	raw, code, err := c.EncodeDecision(hooks.Decision{
		Event: axon.EventPermissionRequest, Action: hooks.ActionRewrite, Rewrite: map[string]any{"command": "ls -la"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 3 {
		t.Fatalf("exit = %d, want 3 from host.yaml", code)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	hso, _ := m["hookSpecificOutput"].(map[string]any)
	dec, _ := hso["decision"].(map[string]any)
	if dec["behavior"] != "allow" {
		t.Fatalf("decision = %v", dec)
	}
	if _, ok := dec["updatedInput"]; !ok {
		t.Fatalf("missing updatedInput: %v", dec)
	}
}

func TestEncodeDecisionPostToolUseBlock(t *testing.T) {
	c := New()
	raw, code, err := c.EncodeDecision(hooks.Decision{Event: axon.EventPostToolUse, Action: hooks.ActionBlock, Message: "post-check failed"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if err := hooks.ValidateDecision(axon.HostClaude, axon.EventPostToolUse, hooks.ActionBlock, raw); err != nil {
		t.Fatal(err)
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
	if err := hooks.ValidateDecision(axon.HostClaude, axon.EventSessionStart, hooks.ActionAllow, raw); err != nil {
		t.Fatal(err)
	}
}

// TestSessionStartAllowContextRoundTrip: the fixture carries context
// injection (additionalContext, systemMessage, continue) — the entire
// point of a SessionStart hook. Decode must populate Decision.Metadata
// with all three, and re-encoding that Decision must reproduce the
// fixture byte-for-byte (JSON-value-equal), not just validate.
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
		"additional_context": "loaded project context",
		"system_message":     "session ready",
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
	if err := hooks.ValidateDecision(axon.HostClaude, axon.EventSessionStart, hooks.ActionAllow, again); err != nil {
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

func TestPermissionRequestInputRoundTrip(t *testing.T) {
	c := New()
	raw := fixture(t, "PermissionRequest.input.json")
	if err := hooks.ValidateInput(axon.HostClaude, axon.EventPermissionRequest, raw); err != nil {
		t.Fatal(err)
	}
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

func TestPermissionRequestDecisionFixturesValidateAndDecode(t *testing.T) {
	c := New()
	cases := []struct {
		file   string
		action hooks.Action
	}{
		{"PermissionRequest.allow.json", hooks.ActionAllow},
		{"PermissionRequest.block.json", hooks.ActionBlock},
	}
	for _, tc := range cases {
		raw := fixture(t, tc.file)
		if err := hooks.ValidateDecision(axon.HostClaude, axon.EventPermissionRequest, tc.action, raw); err != nil {
			t.Fatalf("%s: %v", tc.file, err)
		}
		d, err := c.DecodeDecision(raw, 0)
		if err != nil {
			t.Fatalf("%s: DecodeDecision: %v", tc.file, err)
		}
		if d.Action != tc.action {
			t.Fatalf("%s: action = %v, want %v", tc.file, d.Action, tc.action)
		}
	}
}

func TestPermissionRequestBlockFixtureDecodesEvent(t *testing.T) {
	c := New()
	d, err := c.DecodeDecision(fixture(t, "PermissionRequest.block.json"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Event != axon.EventPermissionRequest {
		t.Fatalf("Event = %q, want %q", d.Event, axon.EventPermissionRequest)
	}
	if d.Message == "" {
		t.Fatalf("Message empty")
	}
}

// TestEncodeInputRequiresNativeOrClose pins I2: EncodeInput builds a
// payload Claude itself would have emitted, so it must require the event
// be native or close, not merely classified. Claude's capability file is
// all-native today, so the looseness is latent: this test constructs the
// divergence directly rather than waiting for a synthesized row to be
// added and the bug to go live silently.
func TestEncodeInputRequiresNativeOrClose(t *testing.T) {
	c := &codec{
		host: mustHost(t),
		caps: hooks.Capabilities{
			Host:        "claude",
			Native:      []hooks.Pair{{HostEvent: "PreToolUse", Event: axon.EventPreToolUse}},
			Synthesized: map[axon.Event]hooks.Recipe{axon.EventFileChanged: {Source: "PostToolUse"}},
			Unsupported: []axon.Event{axon.EventTeammateIdle},
		},
	}
	for _, ev := range []axon.Event{axon.EventFileChanged, axon.EventTeammateIdle} {
		out, err := c.EncodeInput(hooks.Input{Event: ev, SessionID: "s", Cwd: "/tmp"})
		if err == nil {
			t.Fatalf("EncodeInput(%q) returned %s with nil error; want ErrUnsupportedEvent", ev, out)
		}
		if !errors.Is(err, hooks.ErrUnsupportedEvent) {
			t.Fatalf("EncodeInput(%q) error = %v; want ErrUnsupportedEvent", ev, err)
		}
	}
}

func mustHost(t *testing.T) axon.Host {
	t.Helper()
	h, ok := axon.Get(axon.HostClaude)
	if !ok {
		t.Fatal("claude host missing from spec")
	}
	return h
}
