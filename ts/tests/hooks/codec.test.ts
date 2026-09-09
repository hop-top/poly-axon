import { describe, expect, it } from "vitest";
import { codecFor, registeredHosts } from "../../src/hooks";
import "../../src/hooks/hosts/all";

describe("codec registry", () => {
  it("codecFor resolves canonical names only, never an alias", () => {
    expect(codecFor("claude")).toBeDefined();
    // "claude-code" is a published alias of claude, not a canonical name --
    // codecFor must not resolve it (mirrors hooks.For in Go, which keys
    // its map by canonical host name only).
    expect(codecFor("claude-code")).toBeUndefined();
  });

  it("codecFor returns undefined for an unknown host", () => {
    expect(codecFor("not-a-real-host")).toBeUndefined();
  });

  it("registeredHosts is exactly the four hooked hosts with a codec today, sorted", () => {
    expect(registeredHosts()).toEqual(["claude", "codex", "gemini", "opencode"]);
  });

  it("every registered codec's host() matches its registry key", () => {
    for (const name of registeredHosts()) {
      expect(codecFor(name)?.host()).toBe(name);
    }
  });
});
