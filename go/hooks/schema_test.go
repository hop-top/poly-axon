package hooks

import (
	"errors"
	"testing"

	"hop.top/axon"
)

func TestValidateInputMissingSchemaIsErrSchema(t *testing.T) {
	err := ValidateInput(axon.HostClaude, axon.Event("NoSuchEvent"), []byte(`{}`))
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("got %v, want ErrSchema", err)
	}
}

// TestAllowSchemasRejectBlockPayloads pins I5: claude's and codex's allow
// is {} on the wire, so an allow schema that accepts any object accepts a
// block payload too — ValidateDecision then passes a deny through as an
// allow. The allow schemas must be closed.
func TestAllowSchemasRejectBlockPayloads(t *testing.T) {
	block := []byte(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"no"}}`)
	topLevelBlock := []byte(`{"decision":"block","reason":"no"}`)
	for _, host := range []string{"claude", "codex"} {
		t.Run(host+"/PreToolUse", func(t *testing.T) {
			if err := ValidateDecision(host, axon.EventPreToolUse, ActionAllow, []byte(`{}`)); err != nil {
				t.Fatalf("allow fixture {} must still validate: %v", err)
			}
			if err := ValidateDecision(host, axon.EventPreToolUse, ActionAllow, block); err == nil {
				t.Fatalf("PreToolUse allow schema accepted a block payload; want ErrSchema")
			}
		})
		t.Run(host+"/SessionStart", func(t *testing.T) {
			if err := ValidateDecision(host, axon.EventSessionStart, ActionAllow, topLevelBlock); err == nil {
				t.Fatalf("SessionStart allow schema accepted a block payload; want ErrSchema")
			}
		})
	}
	// claude's PermissionRequest allow is {} on the wire too, and its
	// block shape is hookSpecificOutput.decision.behavior. codex has no
	// PermissionRequest channel, so there is no copy to fix there.
	t.Run("claude/PermissionRequest", func(t *testing.T) {
		if err := ValidateDecision("claude", axon.EventPermissionRequest, ActionAllow, []byte(`{}`)); err != nil {
			t.Fatalf("allow fixture {} must still validate: %v", err)
		}
		deny := []byte(`{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"deny","message":"no"}}}`)
		if err := ValidateDecision("claude", axon.EventPermissionRequest, ActionAllow, deny); err == nil {
			t.Fatalf("PermissionRequest allow schema accepted a block payload; want ErrSchema")
		}
	})
	// gemini and opencode close their allow schemas a different way —
	// required + an enum pinning the decision field to "allow" — so a
	// block payload already fails. Asserted here so the two styles stay
	// equivalent rather than one silently regressing.
	t.Run("gemini/PreToolUse", func(t *testing.T) {
		if err := ValidateDecision("gemini", axon.EventPreToolUse, ActionAllow,
			[]byte(`{"decision":"deny","reason":"no"}`)); err == nil {
			t.Fatal("gemini PreToolUse allow schema accepted a deny payload; want ErrSchema")
		}
	})
	t.Run("opencode/PreToolUse", func(t *testing.T) {
		if err := ValidateDecision("opencode", axon.EventPreToolUse, ActionAllow,
			[]byte(`{"action":"block","message":"no"}`)); err == nil {
			t.Fatal("opencode PreToolUse allow schema accepted a block payload; want ErrSchema")
		}
	})
}
