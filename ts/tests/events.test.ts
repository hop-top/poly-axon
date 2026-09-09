import { describe, expect, it } from "vitest";
import { events, nativeEvents } from "../src/index";

// Mirrors hop.top/axon's events_test.go case for case.

describe("events", () => {
  it("catalog totals 32 events; nativeEvents() is 26 (TestEventsCatalogLoads)", () => {
    expect(events()).toHaveLength(32);
    expect(nativeEvents()).toHaveLength(26);
  });

  it("non-native events are excluded from nativeEvents (TestNonNativeEventsExcludedFromNativeScope)", () => {
    // Origin is the whole native test, so every non-native event must be
    // reachable through origin alone -- no category carve-out backs it up.
    const native = new Set(nativeEvents());
    const nonNative = events().filter((e) => (e.origin ?? "native") !== "native");
    expect(nonNative).toHaveLength(6); // 4 extension + 2 derived
    for (const e of nonNative) {
      expect(native.has(e.name), `${e.origin} event ${e.name} must not appear in nativeEvents`).toBe(false);
    }
  });

  it("every event declares origin explicitly (TestEveryEventDeclaresOriginExplicitly)", () => {
    // effectiveOrigin() still defaults a missing origin to native, but
    // nothing in the catalog relies on that fallback.
    for (const e of events()) {
      expect(e.origin, `event ${e.name} omits origin`).not.toBeUndefined();
    }
  });

  it("nativeEvents is exactly the origin-native subset (contract notes 2c)", () => {
    const byOriginAlone = events().filter((e) => (e.origin ?? "native") === "native");
    expect(byOriginAlone).toHaveLength(nativeEvents().length);
    expect(byOriginAlone.map((e) => e.name)).toEqual(nativeEvents());
  });
});
