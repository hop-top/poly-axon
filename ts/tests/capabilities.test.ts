import { describe, expect, it } from "vitest";
import { hosts, hookedHosts, nativeEvents, loadCapabilities, UnknownHostError, SchemaError, get } from "../src/index";

// Mirrors hop.top/axon/hooks's capabilities_test.go case for case, to the
// extent this task's scope (identity + events, no codecs) covers it.

describe("capabilities", () => {
  it("every native event is classified exactly once per hooked host (TestEveryNativeEventClassifiedOnce)", () => {
    for (const h of hookedHosts()) {
      const caps = loadCapabilities(h.name);
      for (const ev of nativeEvents()) {
        let n = 0;
        if (caps.native.some((p) => p.event === ev)) n++;
        if (caps.close?.some((p) => p.event === ev)) n++;
        if (caps.synthesized && ev in caps.synthesized) n++;
        if (caps.unsupported?.includes(ev)) n++;
        expect(n, `${h.name}: event ${ev} classified ${n} times, want exactly 1`).toBe(1);
      }
    }
  });

  it("claude is all-native (TestClaudeIsAllNative)", () => {
    const caps = loadCapabilities("claude");
    expect(caps.native).toHaveLength(26);
    expect(Object.keys(caps.synthesized ?? {})).toHaveLength(0);
    expect(caps.unsupported ?? []).toHaveLength(0);
  });

  // Go's hooks.LoadCapabilities returns ErrUnknownHost ONLY from
  // axon.Get's not-found branch (hooks/capabilities.go:95-97); a
  // recognized host whose capabilities.yaml can't be loaded falls
  // through to axon.LoadSpecYAML's own error (hooks/capabilities.go:99-101),
  // which is never re-labeled as ErrUnknownHost. Verified directly against
  // hooks/capabilities.go before pinning this split.
  it("identity-only hosts have no capabilities, and the error is NOT unknown-host (TestIdentityOnlyHostsHaveNoCapabilities)", () => {
    for (const h of hosts()) {
      if (h.hooks) continue;
      expect(() => loadCapabilities(h.name), h.name).toThrow(SchemaError);
      expect(() => loadCapabilities(h.name), h.name).not.toThrow(UnknownHostError);
    }
  });

  it("loadCapabilities throws UnknownHostError for a name axon does not recognize", () => {
    expect(() => loadCapabilities("not-a-real-host")).toThrow(UnknownHostError);
  });

  it("loadCapabilities distinguishes unknown-host from recognized-but-no-capabilities (amp)", () => {
    // amp is a real, recognized host (identity-only: hooks: false, no
    // capabilities.yaml on disk) -- it must never be reported as unknown.
    expect(get("amp")).toBeDefined();
    let err: unknown;
    try {
      loadCapabilities("amp");
    } catch (e) {
      err = e;
    }
    expect(err).toBeInstanceOf(SchemaError);
    expect(err).not.toBeInstanceOf(UnknownHostError);
    expect((err as Error).message).toContain("amp");
    expect((err as Error).message).toContain("capabilities.yaml");

    let unknownErr: unknown;
    try {
      loadCapabilities("not-a-real-host");
    } catch (e) {
      unknownErr = e;
    }
    expect(unknownErr).toBeInstanceOf(UnknownHostError);
    expect((unknownErr as Error).message).toContain("not-a-real-host");
  });
});
