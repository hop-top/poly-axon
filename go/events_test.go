package axon

import "testing"

func TestEventsCatalogLoads(t *testing.T) {
	evs := Events()
	if len(evs) != 32 {
		t.Fatalf("got %d events, want 32 (26 native + 6 extension/derived)", len(evs))
	}
	if len(NativeEvents()) != 26 {
		t.Fatalf("got %d native events, want 26", len(NativeEvents()))
	}
}

func TestGeneratedConstantsMatchCatalog(t *testing.T) {
	want := map[Event]bool{}
	for _, e := range Events() {
		want[e.Name] = true
	}
	for _, c := range allEventConstants { // defined in events_gen.go
		if !want[c] {
			t.Errorf("constant %q not in catalog", c)
		}
		delete(want, c)
	}
	for missing := range want {
		t.Errorf("catalog event %q has no constant", missing)
	}
}

// Origin is the whole native test, so every non-native event must be
// reachable through Origin alone -- no category carve-out backs it up.
func TestNonNativeEventsExcludedFromNativeScope(t *testing.T) {
	native := map[Event]bool{}
	for _, n := range NativeEvents() {
		native[n] = true
	}
	nonNative := 0
	for _, e := range Events() {
		if e.EffectiveOrigin() == "native" {
			continue
		}
		nonNative++
		if native[e.Name] {
			t.Errorf("%s event %q must not appear in NativeEvents", e.EffectiveOrigin(), e.Name)
		}
	}
	if nonNative != 6 {
		t.Fatalf("got %d non-native events, want 6 (4 extension + 2 derived)", nonNative)
	}
}

// Every catalog event declares origin explicitly. EffectiveOrigin still
// defaults a blank to native, but nothing in the catalog relies on it.
func TestEveryEventDeclaresOriginExplicitly(t *testing.T) {
	for _, e := range Events() {
		if e.Origin == "" {
			t.Errorf("event %q omits origin", e.Name)
		}
	}
}
