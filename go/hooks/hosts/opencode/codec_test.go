package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hop.top/axon"
	"hop.top/axon/hooks"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "spec", "fixtures", "hosts", "opencode", name)) //nolint:gosec // test fixture path, name is a literal from call sites in this file
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEncodeInputPreToolUseValidates(t *testing.T) {
	c := New()
	in := hooks.Input{Event: axon.EventPreToolUse, SessionID: "s1",
		ToolName: "Bash", ToolInput: map[string]any{"command": "echo hi"}}
	raw, err := c.EncodeInput(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := hooks.ValidateInput(axon.HostOpencode, axon.EventPreToolUse, raw); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["type"] != "tool.execute.before" || m["tool"] != "Bash" {
		t.Fatalf("envelope = %v", m)
	}
	if _, ok := m["tool_name"]; ok {
		t.Fatalf("envelope must not carry hook_event_name-style field tool_name: %v", m)
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

func TestFileChangedRoundTrip(t *testing.T) {
	c := New()
	raw := fixture(t, "FileChanged.input.json")
	in, err := c.DecodeInput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if in.Extra["path"] != "/tmp/axon/file.txt" {
		t.Fatalf("Extra[path] = %v", in.Extra["path"])
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

func TestDecodeDecisionBlock(t *testing.T) {
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
	if code != 0 {
		t.Fatalf("exit = %d, want 0: opencode has no exit-code contract", code)
	}
	if err := hooks.ValidateDecision(axon.HostOpencode, axon.EventPreToolUse, hooks.ActionBlock, raw); err != nil {
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
		t.Fatalf("exit = %d, want 0", code)
	}
	if err := hooks.ValidateDecision(axon.HostOpencode, axon.EventPreToolUse, hooks.ActionAllow, raw); err != nil {
		t.Fatal(err)
	}
}

func TestEncodeDecisionRewriteValidates(t *testing.T) {
	c := New()
	raw, code, err := c.EncodeDecision(hooks.Decision{Action: hooks.ActionRewrite, Rewrite: map[string]any{"command": "echo safe"}})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if err := hooks.ValidateDecision(axon.HostOpencode, axon.EventPreToolUse, hooks.ActionRewrite, raw); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if _, ok := m["rewrite"]; !ok {
		t.Fatalf("rewrite must travel under \"rewrite\" (xat tool.execute.before-rewrite.json): %v", m)
	}
}

func TestEncodeDecisionWarnDoesNotDegradeToBlock(t *testing.T) {
	c := New()
	raw, code, err := c.EncodeDecision(hooks.Decision{Action: hooks.ActionWarn, Message: "careful"})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if err := hooks.ValidateDecision(axon.HostOpencode, axon.EventPreToolUse, hooks.ActionWarn, raw); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["action"] != "warn" {
		t.Fatalf("warn must not degrade to another action: %v", m)
	}
	if m["message"] != "careful" {
		t.Fatalf("warn message lost: %v", m)
	}
}

func TestDecodeDecisionWarn(t *testing.T) {
	c := New()
	d, err := c.DecodeDecision(fixture(t, "PreToolUse.warn.json"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != hooks.ActionWarn {
		t.Fatalf("decision = %+v, want warn (must not decode to block)", d)
	}
	if d.Message == "" {
		t.Fatalf("decision = %+v, want a message", d)
	}
}

func TestDecodeDecisionEmptyStdoutAllows(t *testing.T) {
	c := New()
	d, err := c.DecodeDecision([]byte(""), 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != hooks.ActionAllow {
		t.Fatalf("decision = %+v, want allow for empty stdout", d)
	}
}

func TestDecodeDecisionIgnoresExitArgument(t *testing.T) {
	c := New()
	// Non-zero exit must not influence the decoded decision: opencode has
	// no exit-code contract, only stdout JSON matters.
	d1, err := c.DecodeDecision(fixture(t, "PreToolUse.block.json"), 0)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := c.DecodeDecision(fixture(t, "PreToolUse.block.json"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if d1.Action != d2.Action || d1.Message != d2.Message {
		t.Fatalf("decision changed with exit code: %+v vs %+v", d1, d2)
	}
}

func equalJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
