import { describe, expect, it } from "vitest";
import { hosts, get, resolve, hookedHosts } from "../src/index";

// Mirrors hop.top/axon's registry_test.go case for case.

describe("registry", () => {
  it("hosts count is 17 (TestHostsCount)", () => {
    expect(hosts()).toHaveLength(17);
  });

  it("hosts() is sorted by canonical name", () => {
    const names = hosts().map((h) => h.name);
    const sorted = [...names].sort();
    expect(names).toEqual(sorted);
  });

  it("get rejects aliases; only resolve is alias-aware (TestGetIsCanonicalOnly)", () => {
    expect(get("claude-code")).toBeUndefined();
    expect(get("gemini-cli")).toBeUndefined();
    expect(get("codex-cli")).toBeUndefined();
    const h = get("claude");
    expect(h?.name).toBe("claude");
  });

  it("resolve accepts canonical names and published aliases (TestResolveAliases)", () => {
    const cases: Record<string, string> = {
      "claude-code": "claude",
      "gemini-cli": "gemini",
      "codex-cli": "codex",
      opencode: "opencode",
    };
    for (const [alias, want] of Object.entries(cases)) {
      const h = resolve(alias);
      expect(h?.name, `resolve(${alias})`).toBe(want);
    }
  });

  it("resolve of an unknown name returns undefined (TestResolveAliases)", () => {
    expect(resolve("nope")).toBeUndefined();
  });

  it("aliases are unique across all hosts, and never collide with a canonical name (TestAliasUniqueness)", () => {
    const seen = new Map<string, string>();
    for (const h of hosts()) seen.set(h.name, h.name);
    for (const h of hosts()) {
      for (const a of h.aliases) {
        const owner = seen.get(a);
        expect(owner, `alias ${a} of ${h.name} collides with ${owner}`).toBeUndefined();
        seen.set(a, h.name);
      }
    }
  });

  it("hooked hosts count is 8 (TestHookedHostsAreEight)", () => {
    expect(hookedHosts()).toHaveLength(8);
  });

  it("hooked hosts all carry hooks: true", () => {
    for (const h of hookedHosts()) {
      expect(h.hooks).toBe(true);
    }
  });
});
