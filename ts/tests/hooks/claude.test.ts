import { describe, expect, it } from "vitest";
import { validate, UnsupportedActionError, UnsupportedEventError } from "../../src/index";
import { codecFor } from "../../src/hooks";
import "../../src/hooks/hosts/all";
import { fixture } from "./testutil";

// Pins the eight task-4 invariants against the claude codec. Matched
// against hooks/hosts/claude/codec.go and hooks/hosts/claude/codec_test.go.

const codec = () => {
  const c = codecFor("claude");
  if (!c) throw new Error("claude codec not registered");
  return c;
};

describe("claude codec", () => {
  it("host() returns the canonical name", () => {
    expect(codec().host()).toBe("claude");
  });

  // Invariant 2: unknown/empty action on encode throws UnsupportedActionError.
  it("I2: encodeDecision throws UnsupportedActionError for an unrecognized action", () => {
    expect(() => codec().encodeDecision({ action: "bogus" as never })).toThrow(UnsupportedActionError);
  });

  it("I2: encodeDecision never maps warn to block on PreToolUse", () => {
    const { stdout } = codec().encodeDecision({ action: "warn" });
    const hso = (stdout as Record<string, unknown>)?.hookSpecificOutput as Record<string, unknown> | undefined;
    expect(hso?.permissionDecision).toBe("ask");
    expect(hso?.permissionDecision).not.toBe("deny");
  });

  // Invariant 3: encodeDecision rejects an event the capability map does
  // not mark native/close.
  it("I3: encodeDecision throws UnsupportedEventError for an event claude does not classify as native/close", () => {
    expect(() => codec().encodeDecision({ event: "TeammateIdle", action: "block" })).toThrow(UnsupportedEventError);
  });

  it("I3: encodeDecision accepts a blank event (default PreToolUse shape)", () => {
    expect(() => codec().encodeDecision({ action: "allow" })).not.toThrow();
  });

  // Regression: Go's `switch d.Event { case "", axon.EventPreToolUse: ... }`
  // treats an explicit empty string exactly like an omitted field -- both
  // must produce the identical PreToolUse shape, never
  // UnsupportedEventError.
  it("regression: an explicit event: \"\" behaves identically to an omitted event", () => {
    const withEmptyString = codec().encodeDecision({ event: "", action: "block", message: "no" });
    const omitted = codec().encodeDecision({ action: "block", message: "no" });
    expect(withEmptyString).toEqual(omitted);
    const hso = (withEmptyString.stdout as Record<string, unknown>).hookSpecificOutput as Record<string, unknown>;
    expect(hso.permissionDecision).toBe("deny");
  });

  // Invariant 4: extra never overwrites a canonical envelope field.
  it("I4: extra cannot clobber the event discriminator or session id", () => {
    const out = codec().encodeInput({
      event: "PreToolUse",
      sessionId: "real-session",
      cwd: "/work",
      toolName: "Bash",
      toolInput: { command: "echo hi" },
      extra: { hook_event_name: "Bogus", session_id: "fake-session", cwd: "/nope" },
    }) as Record<string, unknown>;
    expect(out.hook_event_name).toBe("PreToolUse");
    expect(out.session_id).toBe("real-session");
    expect(out.cwd).toBe("/work");
  });

  // Invariant 1: native-wins decode. Claude's capability file is
  // all-native, so this is exercised generically: every native host event
  // decodes to its own canonical event, never anything else.
  it("I1: PreToolUse.input fixture decodes to PreToolUse (native row)", () => {
    const raw = fixture("claude", "PreToolUse.input.json");
    const input = codec().decodeInput(raw);
    expect(input.event).toBe("PreToolUse");
  });

  // Invariant 5: decodeDecision falls back to exit code on empty stdout,
  // on {}, and on unrecognized JSON -- all three, each with a block exit.
  it("I5: decodeDecision falls back to exit code on empty stdout", () => {
    const decision = codec().decodeDecision("", 2);
    expect(decision.action).toBe("block");
  });

  it("I5: decodeDecision falls back to exit code on {}", () => {
    const decision = codec().decodeDecision({}, 2);
    expect(decision.action).toBe("block");
  });

  it("I5: decodeDecision falls back to exit code on unrecognized JSON", () => {
    const decision = codec().decodeDecision({ unrelated: 1 }, 2);
    expect(decision.action).toBe("block");
  });

  it("I5: decodeDecision falls back to allow's exit code when stdout is empty and exit is 0", () => {
    const decision = codec().decodeDecision("", 0);
    expect(decision.action).toBe("allow");
  });

  // Invariant 6: wire shape varies by event; decision.event selects the
  // shape on encode and is set on decode when the wire reveals it.
  it("I6: encodeDecision picks PermissionRequest's shape when event is PermissionRequest", () => {
    const { stdout } = codec().encodeDecision({ event: "PermissionRequest", action: "block", message: "no" });
    const hso = (stdout as Record<string, unknown>).hookSpecificOutput as Record<string, unknown>;
    const decision = hso.decision as Record<string, unknown>;
    expect(decision.behavior).toBe("deny");
    expect(decision.message).toBe("no");
  });

  it("I6: encodeDecision picks PostToolUse's shape when event is PostToolUse", () => {
    const { stdout } = codec().encodeDecision({ event: "PostToolUse", action: "block", message: "post-check failed" });
    expect(stdout).toEqual({ decision: "block", reason: "post-check failed" });
  });

  it("I6: decodeDecision sets event from hookSpecificOutput.hookEventName", () => {
    const decision = codec().decodeDecision(fixture("claude", "PermissionRequest.block.json"), 0);
    expect(decision.event).toBe("PermissionRequest");
    expect(decision.message).toBe("blocked by policy");
  });

  // decodeDecision must carry the wire's message/rewrite payload through,
  // not just classify the action -- fixtures.test.ts's round-trip only
  // asserts .action (per contract notes §6, decision fixtures are not
  // required to be value-equal after re-encode), so these fields need
  // their own assertion here or a dropped field slips past every test.
  it("decodeDecision carries the PreToolUse.block fixture's message through", () => {
    const decision = codec().decodeDecision(fixture("claude", "PreToolUse.block.json"), 2);
    expect(decision.action).toBe("block");
    expect(decision.message).toBe("blocked by policy");
  });

  it("decodeDecision carries the PreToolUse.rewrite fixture's rewrite payload through", () => {
    const decision = codec().decodeDecision(fixture("claude", "PreToolUse.rewrite.json"), 3);
    expect(decision.action).toBe("rewrite");
    expect(decision.rewrite).toEqual({ command: "echo hello --safe" });
  });

  it("decodeDecision carries the PostToolUse.block fixture's message through", () => {
    const decision = codec().decodeDecision(fixture("claude", "PostToolUse.block.json"), 2);
    expect(decision.action).toBe("block");
    expect(decision.message).toBe("post-check failed");
  });

  // Invariant 7: claude SessionStart allow decisions carry context through
  // metadata; a contextless allow is exactly {}.
  it("I7: contextless SessionStart allow encodes to exactly {}", () => {
    const { stdout } = codec().encodeDecision({ event: "SessionStart", action: "allow" });
    expect(stdout).toEqual({});
  });

  it("I7: SessionStart.allow fixture round-trips through metadata byte-equal", () => {
    const raw = fixture("claude", "SessionStart.allow.json");
    const decision = codec().decodeDecision(raw, 0);
    expect(decision.action).toBe("allow");
    expect(decision.event).toBe("SessionStart");
    expect(decision.metadata).toEqual({
      additional_context: "loaded project context",
      system_message: "session ready",
      continue: true,
    });
    decision.event = "SessionStart";
    const { stdout } = codec().encodeDecision(decision);
    expect(stdout).toEqual(raw);
    validate("claude/SessionStart.allow.json", "hosts/claude/hooks/SessionStart.allow.schema.json", stdout);
  });

  // Invariant 8 does not apply to claude (it has an exit-code contract).
  it("I8 (n/a for claude): claude DOES have an exit-code contract", () => {
    const { exit } = codec().encodeDecision({ action: "block" });
    expect(exit).toBe(2);
  });
});
