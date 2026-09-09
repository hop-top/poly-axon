import { describe, expect, it } from "vitest";
import { UnsupportedActionError, UnsupportedEventError } from "../../src/index";
import { codecFor } from "../../src/hooks";
import "../../src/hooks/hosts/all";
import { fixture } from "./testutil";

// Pins the eight task-4 invariants against the gemini codec. Matched
// against hooks/hosts/gemini/codec.go and codec_test.go.

const codec = () => {
  const c = codecFor("gemini");
  if (!c) throw new Error("gemini codec not registered");
  return c;
};

describe("gemini codec", () => {
  it("host() returns the canonical name", () => {
    expect(codec().host()).toBe("gemini");
  });

  it("I2: encodeDecision throws UnsupportedActionError for an unrecognized action", () => {
    expect(() => codec().encodeDecision({ action: "bogus" as never })).toThrow(UnsupportedActionError);
  });

  // Gemini has no native warn; nerv folds warn into decision:"allow" with
  // reason/systemMessage duplicated -- it must never become "deny".
  it("I2: encodeDecision never maps warn to block/deny", () => {
    const { stdout } = codec().encodeDecision({ action: "warn", message: "careful" });
    expect((stdout as Record<string, unknown>).decision).toBe("allow");
  });

  it("I3: encodeDecision throws UnsupportedEventError for an event gemini does not classify as native/close", () => {
    expect(() => codec().encodeDecision({ event: "TeammateIdle", action: "block" })).toThrow(UnsupportedEventError);
  });

  it("I4: extra cannot clobber the event discriminator or session id", () => {
    const out = codec().encodeInput({
      event: "PreToolUse",
      sessionId: "real-session",
      cwd: "/work",
      toolName: "Bash",
      toolInput: { command: "echo hi" },
      extra: { hook_event_name: "Bogus", session_id: "fake-session" },
    }) as Record<string, unknown>;
    // Gemini's native name for PreToolUse is BeforeTool, from the
    // capability map, never a hard-coded literal.
    expect(out.hook_event_name).toBe("BeforeTool");
    expect(out.session_id).toBe("real-session");
  });

  // Invariant 1: native-wins decode. Gemini lists host event SessionEnd
  // twice -- native -> SessionEnd, close -> Stop -- and only the native
  // row is what Gemini actually emits.
  it("I1: host event SessionEnd decodes to canonical SessionEnd (native row wins over close row)", () => {
    const input = codec().decodeInput({ hook_event_name: "SessionEnd", session_id: "s", cwd: "/tmp" });
    expect(input.event).toBe("SessionEnd");
  });

  // Gemini's DecodeDecision now has the same "recognized shape" gate as
  // claude/codex: empty stdout OR JSON with none of Gemini's known
  // decision keys both fall back to the exit code. This corrects a
  // fail-open bug this suite used to pin (a hook exiting with the block
  // code and printing `{}` was silently read as allow) -- verified
  // directly against the fixed Go reference (hooks/hosts/gemini/codec.go's
  // DecodeDecision / decodeKnownShape) before updating this test:
  // decodeDecision({}, 2) now returns block, matching Go, in both
  // languages.
  it("I5: decodeDecision falls back to exit code on empty stdout OR unrecognized JSON", () => {
    expect(codec().decodeDecision("", 2).action).toBe("block");
    expect(codec().decodeDecision("", 0).action).toBe("allow");
  });

  it("I5 (recognized-shape gate): {} with a block exit now falls back to block, not allow", () => {
    expect(codec().decodeDecision({}, 2).action).toBe("block");
  });

  it("I5 (recognized-shape gate): unrecognized JSON with a block exit now falls back to block", () => {
    expect(codec().decodeDecision({ unrelated: 1 }, 2).action).toBe("block");
  });

  it("I5 (recognized-shape gate): a recognized shape still wins over a disagreeing exit code", () => {
    expect(codec().decodeDecision({ decision: "deny", reason: "no" }, 0).action).toBe("block");
  });

  // Invariant 6: gemini does not vary wire shape by event -- a blank
  // event is correct.
  it("I6: encodeDecision produces the same shape regardless of decision.event", () => {
    const blank = codec().encodeDecision({ action: "allow" });
    const withEvent = codec().encodeDecision({ event: "PreToolUse", action: "allow" });
    expect(blank).toEqual(withEvent);
  });

  // Regression: gemini's shape never varies by event, but an explicit
  // event: "" must still be accepted exactly like an omitted event (never
  // UnsupportedEventError) -- checkEvent already guards `ev === ""`
  // alongside `ev === undefined`; this pins it against the claude/codex
  // pattern regressing here too.
  it("regression: an explicit event: \"\" is accepted exactly like an omitted event", () => {
    const withEmptyString = codec().encodeDecision({ event: "", action: "allow" });
    const omitted = codec().encodeDecision({ action: "allow" });
    expect(withEmptyString).toEqual(omitted);
  });

  it("I6: decodeDecision never sets decision.event (gemini's wire never reveals it)", () => {
    const decision = codec().decodeDecision(fixture("gemini", "PreToolUse.block.json"), 2);
    expect(decision.event).toBeUndefined();
    expect(decision.action).toBe("block");
    expect(decision.message).toBe("blocked by policy");
  });

  // No PreToolUse.rewrite fixture exists on disk for gemini (rewrite is a
  // real decode path -- hookSpecificOutput.tool_input -- but has never
  // been captured from the host), so this pins it against a literal
  // payload shaped the way encodeDecision's own rewrite branch produces
  // it, rather than skipping decode-side rewrite coverage entirely.
  it("decodeDecision carries the rewrite payload through from hookSpecificOutput.tool_input", () => {
    const decision = codec().decodeDecision(
      { decision: "allow", hookSpecificOutput: { tool_input: { command: "echo safe" } } },
      0,
    );
    expect(decision.action).toBe("rewrite");
    expect(decision.rewrite).toEqual({ command: "echo safe" });
  });

  // Invariant 7 does not apply: gemini has no SessionStart context shape.
  it("I7 (n/a for gemini): SessionStart allow fixture decodes to plain allow, no metadata", () => {
    const decision = codec().decodeDecision(fixture("gemini", "SessionStart.allow.json"), 0);
    expect(decision.action).toBe("allow");
    expect(decision.metadata ?? {}).toEqual({});
  });

  // Invariant 8 does not apply to gemini (it has an exit-code contract,
  // though only allow/block are declared).
  it("I8 (n/a for gemini): gemini DOES have an exit-code contract for block", () => {
    const { exit } = codec().encodeDecision({ action: "block", message: "no" });
    expect(exit).toBe(2);
  });

  it("warn fold: PreToolUse.warn fixture decodes to warn only via the systemMessage tell", () => {
    const decision = codec().decodeDecision(fixture("gemini", "PreToolUse.warn.json"), 0);
    expect(decision.action).toBe("warn");
    expect(decision.message).toBe("consider a safer flag");
  });

  it("warn fold: same reason without systemMessage decodes to allow, not warn", () => {
    const decision = codec().decodeDecision({ decision: "allow", reason: "fyi" }, 0);
    expect(decision.action).toBe("allow");
  });
});
