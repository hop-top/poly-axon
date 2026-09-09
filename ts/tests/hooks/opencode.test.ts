import { describe, expect, it } from "vitest";
import { UnsupportedActionError, UnsupportedEventError } from "../../src/index";
import { codecFor } from "../../src/hooks";
import "../../src/hooks/hosts/all";
import { fixture } from "./testutil";

// Pins the eight task-4 invariants against the opencode codec. Matched
// against hooks/hosts/opencode/codec.go.

const codec = () => {
  const c = codecFor("opencode");
  if (!c) throw new Error("opencode codec not registered");
  return c;
};

describe("opencode codec", () => {
  it("host() returns the canonical name", () => {
    expect(codec().host()).toBe("opencode");
  });

  it("I2: encodeDecision throws UnsupportedActionError for an unrecognized action", () => {
    expect(() => codec().encodeDecision({ action: "bogus" as never })).toThrow(UnsupportedActionError);
  });

  // OpenCode passes warn through verbatim; folding it to block would
  // change semantics (blocking a tool the handler only warned about).
  it("I2: encodeDecision never maps warn to block", () => {
    const { stdout } = codec().encodeDecision({ action: "warn", message: "careful" });
    expect((stdout as Record<string, unknown>).action).toBe("warn");
  });

  it("I3: encodeDecision throws UnsupportedEventError for an event opencode does not classify as native/close", () => {
    expect(() => codec().encodeDecision({ event: "TeammateIdle", action: "block" })).toThrow(UnsupportedEventError);
  });

  it("I4: extra cannot clobber the type discriminator or session id", () => {
    const out = codec().encodeInput({
      event: "PreToolUse",
      sessionId: "real-session",
      cwd: "/work",
      toolName: "Bash",
      toolInput: { command: "echo hi" },
      extra: { type: "bogus.event", session_id: "fake-session" },
    }) as Record<string, unknown>;
    expect(out.type).toBe("tool.execute.before");
    expect(out.session_id).toBe("real-session");
  });

  // Invariant 1: native-wins decode. tui.prompt.append and todo.updated
  // are close-only rows (no native row claims them), so they still
  // decode; TaskCompleted's fixture pins that.
  it("I1: TaskCompleted.input fixture (close row todo.updated) decodes to TaskCompleted", () => {
    const input = codec().decodeInput(fixture("opencode", "TaskCompleted.input.json"));
    expect(input.event).toBe("TaskCompleted");
  });

  // Invariant 8: opencode has no exit-code contract. encodeDecision
  // always returns exit 0; decodeDecision ignores the exit argument.
  it("I8: encodeDecision always returns exit 0, for every action", () => {
    expect(codec().encodeDecision({ action: "allow" }).exit).toBe(0);
    expect(codec().encodeDecision({ action: "warn", message: "m" }).exit).toBe(0);
    expect(codec().encodeDecision({ action: "block", message: "m" }).exit).toBe(0);
    expect(codec().encodeDecision({ action: "rewrite", rewrite: { a: 1 } }).exit).toBe(0);
  });

  it("I8: decodeDecision ignores the exit argument entirely", () => {
    const raw = fixture("opencode", "PreToolUse.block.json");
    const withZero = codec().decodeDecision(raw, 0);
    const withNonZero = codec().decodeDecision(raw, 137);
    expect(withZero).toEqual(withNonZero);
    expect(withZero.action).toBe("block");
    expect(withZero.message).toBe("blocked by policy");
  });

  // Both PreToolUse.block.json and PreToolUse.warn.json carry a non-empty
  // "message" on the wire; asserting only .action here would pass even if
  // decodeDecision silently dropped the message (the reviewer's mutation
  // that the parity harness alone caught -- see the fix-round report).
  it("decodeDecision carries the fixture's message through for block", () => {
    const decision = codec().decodeDecision(fixture("opencode", "PreToolUse.block.json"), 0);
    expect(decision.action).toBe("block");
    expect(decision.message).toBe("blocked by policy");
  });

  it("decodeDecision carries the fixture's message through for warn", () => {
    const decision = codec().decodeDecision(fixture("opencode", "PreToolUse.warn.json"), 0);
    expect(decision.action).toBe("warn");
    expect(decision.message).toBe("this command touches a sensitive path");
  });

  it("decodeDecision carries the fixture's rewrite payload through", () => {
    const decision = codec().decodeDecision(fixture("opencode", "PreToolUse.rewrite.json"), 0);
    expect(decision.action).toBe("rewrite");
    expect(decision.rewrite).toEqual({ command: "echo safe" });
  });

  // Invariant 5: the three fallback cases, all "with a block exit code"
  // per the brief -- opencode ignores exit entirely, so all three must
  // still decode to allow (opencode's own empty-stdout/unrecognized
  // default), proving the exit argument plays no role here.
  it("I5 (opencode ignores exit): decodeDecision falls back to allow on empty stdout regardless of exit", () => {
    expect(codec().decodeDecision("", 2).action).toBe("allow");
  });

  it("I5 (opencode ignores exit): decodeDecision falls back to allow on {} regardless of exit", () => {
    expect(codec().decodeDecision({}, 2).action).toBe("allow");
  });

  it("I5 (opencode ignores exit): decodeDecision falls back to allow on unrecognized JSON regardless of exit", () => {
    expect(codec().decodeDecision({ unrelated: 1 }, 2).action).toBe("allow");
  });

  // Invariant 6: opencode does not vary wire shape by event -- a blank
  // event is correct.
  it("I6: encodeDecision produces the same shape regardless of decision.event", () => {
    const blank = codec().encodeDecision({ action: "allow" });
    const withEvent = codec().encodeDecision({ event: "PreToolUse", action: "allow" });
    expect(blank).toEqual(withEvent);
  });

  // Regression: opencode's shape never varies by event, but an explicit
  // event: "" must still be accepted exactly like an omitted event (never
  // UnsupportedEventError) -- checkEvent already guards `ev === ""`
  // alongside `ev === undefined`; this pins it against the claude/codex
  // pattern regressing here too.
  it("regression: an explicit event: \"\" is accepted exactly like an omitted event", () => {
    const withEmptyString = codec().encodeDecision({ event: "", action: "allow" });
    const omitted = codec().encodeDecision({ action: "allow" });
    expect(withEmptyString).toEqual(omitted);
  });

  it("I6: decodeDecision never sets decision.event (opencode's wire never reveals it)", () => {
    const decision = codec().decodeDecision(fixture("opencode", "PreToolUse.block.json"), 0);
    expect(decision.event).toBeUndefined();
  });

  it("rewrite field name is 'rewrite', not nested under 'input'", () => {
    const { stdout } = codec().encodeDecision({ action: "rewrite", rewrite: { command: "echo safe" } });
    expect(stdout).toEqual({ action: "rewrite", rewrite: { command: "echo safe" } });
  });
});
