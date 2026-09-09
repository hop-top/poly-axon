import { describe, expect, it } from "vitest";
import { validate, UnsupportedActionError, UnsupportedEventError } from "../../src/index";
import { codecFor } from "../../src/hooks";
import "../../src/hooks/hosts/all";
import { fixture } from "./testutil";

// Pins the eight task-4 invariants against the codex codec. Matched
// against hooks/hosts/codex/codec.go.

const codec = () => {
  const c = codecFor("codex");
  if (!c) throw new Error("codex codec not registered");
  return c;
};

describe("codex codec", () => {
  it("host() returns the canonical name", () => {
    expect(codec().host()).toBe("codex");
  });

  it("I2: encodeDecision throws UnsupportedActionError for an unrecognized action", () => {
    expect(() => codec().encodeDecision({ action: "bogus" as never })).toThrow(UnsupportedActionError);
  });

  it("I2: encodeDecision never maps warn to block on PreToolUse", () => {
    const { stdout } = codec().encodeDecision({ action: "warn" });
    const hso = (stdout as Record<string, unknown>).hookSpecificOutput as Record<string, unknown>;
    expect(hso.permissionDecision).toBe("ask");
    expect(hso.permissionDecision).not.toBe("deny");
  });

  it("I3: encodeDecision throws UnsupportedEventError for an event codex does not classify as native/close", () => {
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

  it("I4: extra cannot clobber the event discriminator or session id", () => {
    const out = codec().encodeInput({
      event: "PreToolUse",
      sessionId: "real-session",
      cwd: "/work",
      toolName: "shell",
      toolInput: { command: "echo hi" },
      extra: { hook_event_name: "Bogus", session_id: "fake-session" },
    }) as Record<string, unknown>;
    expect(out.hook_event_name).toBe("PreToolUse");
    expect(out.session_id).toBe("real-session");
  });

  it("I1: PreToolUse.input fixture decodes to PreToolUse (native identity mapping)", () => {
    const input = codec().decodeInput(fixture("codex", "PreToolUse.input.json"));
    expect(input.event).toBe("PreToolUse");
  });

  it("I5: decodeDecision falls back to exit code on empty stdout", () => {
    expect(codec().decodeDecision("", 2).action).toBe("block");
  });

  it("I5: decodeDecision falls back to exit code on {}", () => {
    expect(codec().decodeDecision({}, 2).action).toBe("block");
  });

  it("I5: decodeDecision falls back to exit code on unrecognized JSON", () => {
    expect(codec().decodeDecision({ unrelated: 1 }, 2).action).toBe("block");
  });

  // decodeDecision must carry the wire's message/rewrite payload through,
  // not just classify the action -- fixtures.test.ts's round-trip only
  // asserts .action (contract notes §6), so these fields need their own
  // assertion here or a dropped field slips past every test.
  it("decodeDecision carries the PreToolUse.block fixture's message through", () => {
    const decision = codec().decodeDecision(fixture("codex", "PreToolUse.block.json"), 2);
    expect(decision.action).toBe("block");
    expect(decision.message).toBe("blocked by policy");
  });

  it("decodeDecision carries the PreToolUse.rewrite fixture's rewrite payload through", () => {
    const decision = codec().decodeDecision(fixture("codex", "PreToolUse.rewrite.json"), 3);
    expect(decision.action).toBe("rewrite");
    expect(decision.rewrite).toEqual({ command: "echo hello --safe" });
  });

  it("decodeDecision carries the PostToolUse.block fixture's message through", () => {
    const decision = codec().decodeDecision(fixture("codex", "PostToolUse.block.json"), 2);
    expect(decision.action).toBe("block");
    expect(decision.message).toBe("post-condition failed");
  });

  it("I6: encodeDecision picks PostToolUse's shape when event is PostToolUse", () => {
    const { stdout } = codec().encodeDecision({ event: "PostToolUse", action: "block", message: "post-condition failed" });
    expect(stdout).toEqual({ decision: "block", reason: "post-condition failed" });
  });

  it("I6: UserPromptSubmit and Stop allow with the empty envelope", () => {
    for (const event of ["UserPromptSubmit", "Stop"]) {
      const { stdout } = codec().encodeDecision({ event, action: "allow" });
      expect(stdout).toEqual({});
    }
  });

  it("I6: UserPromptSubmit block is unsupported (no dedicated block shape)", () => {
    expect(() => codec().encodeDecision({ event: "UserPromptSubmit", action: "block" })).toThrow(UnsupportedActionError);
  });

  it("I7: contextless SessionStart allow encodes to exactly {}", () => {
    const { stdout } = codec().encodeDecision({ event: "SessionStart", action: "allow" });
    expect(stdout).toEqual({});
  });

  it("I7: SessionStart.allow fixture round-trips through metadata byte-equal", () => {
    const raw = fixture("codex", "SessionStart.allow.json");
    const decision = codec().decodeDecision(raw, 0);
    expect(decision.action).toBe("allow");
    expect(decision.event).toBe("SessionStart");
    expect(decision.metadata).toEqual({
      additional_context: "session initialized",
      continue: true,
    });
    decision.event = "SessionStart";
    const { stdout } = codec().encodeDecision(decision);
    expect(stdout).toEqual(raw);
    validate("codex/SessionStart.allow.json", "hosts/codex/hooks/SessionStart.allow.schema.json", stdout);
  });

  it("I8 (n/a for codex): codex DOES have an exit-code contract", () => {
    const { exit } = codec().encodeDecision({ action: "block" });
    expect(exit).toBe(2);
  });
});
