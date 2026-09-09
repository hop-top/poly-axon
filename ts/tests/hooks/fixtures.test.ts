import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { specRoot, validate, get } from "../../src/index";
import { codecFor, type Action } from "../../src/hooks";
import "../../src/hooks/hosts/all";

// Mirrors hop.top/axon/hooks/conformance_test.go's
// TestFixturesRoundTripAndValidate over the four registered codecs
// (claude, codex, gemini, opencode -- contract notes §4). Per contract
// notes §6:
//   - input fixtures: decode-then-encode must be JSON-value-equal to the
//     file, then the re-encoded output must validate against the
//     <Event>.input schema.
//   - decision fixtures: decode must report the action the file name
//     names, decode sets decision.event from the file name, re-encode
//     must validate against the <Event>.<action> schema -- value-equality
//     against the original file is explicitly NOT required (a decision
//     fixture is not guaranteed to be what the codec itself would emit;
//     see docs/spec-contract-notes.md §6 and §8 for the stricter
//     byte-equal rule this task's per-codec tests pin separately for the
//     specific cases §8 names, e.g. claude/codex SessionStart.allow).

const HOOKED_HOSTS = ["claude", "codex", "gemini", "opencode"];

const ACTIONS: Action[] = ["allow", "warn", "block", "rewrite"];

function fixtureDir(host: string): string {
  return path.join(specRoot(), "fixtures", "hosts", host);
}

function readJSON(p: string): unknown {
  return JSON.parse(readFileSync(p, "utf8"));
}

describe("fixtures round-trip and validate", () => {
  for (const host of HOOKED_HOSTS) {
    describe(host, () => {
      const dir = fixtureDir(host);
      const entries = readdirSync(dir).filter((f) => f.endsWith(".json"));

      it("has at least one fixture", () => {
        expect(entries.length).toBeGreaterThan(0);
      });

      for (const entry of entries) {
        const stem = entry.slice(0, -".json".length);
        const parts = stem.split(".");
        expect(parts).toHaveLength(2);
        const [event, kind] = parts;

        if (kind === "input") {
          it(`${entry}: decode -> encode round-trips and validates`, () => {
            const codec = codecFor(host);
            expect(codec, `no codec registered for ${host}`).toBeDefined();
            const raw = readJSON(path.join(dir, entry));
            const input = codec!.decodeInput(raw);
            const again = codec!.encodeInput(input);
            expect(again).toEqual(raw);
            validate(`${host}/${entry}`, `hosts/${host}/hooks/${event}.input.schema.json`, again);
          });
        } else {
          const action = kind as Action;
          expect(ACTIONS).toContain(action);
          it(`${entry}: decode reports ${action}, re-encode validates`, () => {
            const codec = codecFor(host);
            expect(codec, `no codec registered for ${host}`).toBeDefined();
            const raw = readJSON(path.join(dir, entry));
            const decision = codec!.decodeDecision(raw, exitFor(host, action));
            expect(decision.action).toBe(action);
            // The fixture's file name carries the event; set it before
            // re-encoding, same as the Go conformance test, so codecs
            // whose wire shape varies by event re-encode the right shape.
            decision.event = event;
            const { stdout } = codec!.encodeDecision(decision);
            validate(`${host}/${entry}`, `hosts/${host}/hooks/${event}.${action}.schema.json`, stdout);
          });
        }
      }
    });
  }
});

// exitFor mirrors hooks/conformance_test.go's exitFor: the host's declared
// exit code for the action, read from host.yaml via the reader (never a
// hard-coded per-host literal table). Hosts with no exit-code contract
// (opencode) pass 0 -- decodeDecision for opencode ignores its exit
// argument entirely.
function exitFor(host: string, action: Action): number {
  const codes = get(host)?.exitCodes;
  if (!codes) return 0;
  switch (action) {
    case "allow":
      return codes.allow;
    case "warn":
      return codes.warn ?? codes.allow;
    case "block":
      return codes.block;
    case "rewrite":
      return codes.rewrite ?? codes.allow;
  }
}
