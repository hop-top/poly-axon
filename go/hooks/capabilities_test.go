package hooks

import (
	"io/fs"
	"testing"

	"hop.top/axon"
)

func TestEveryNativeEventClassifiedOnce(t *testing.T) {
	for _, h := range axon.HookedHosts() {
		caps, err := LoadCapabilities(h.Name)
		if err != nil {
			t.Fatalf("%s: %v", h.Name, err)
		}
		for _, ev := range axon.NativeEvents() {
			n := 0
			for _, p := range caps.Native {
				if p.Event == ev {
					n++
				}
			}
			for _, p := range caps.Close {
				if p.Event == ev {
					n++
				}
			}
			if _, ok := caps.Synthesized[ev]; ok {
				n++
			}
			for _, u := range caps.Unsupported {
				if u == ev {
					n++
				}
			}
			if n != 1 {
				t.Errorf("%s: event %s classified %d times, want exactly 1", h.Name, ev, n)
			}
		}
	}
}

func TestClaudeIsAllNative(t *testing.T) {
	caps, _ := LoadCapabilities(axon.HostClaude)
	if len(caps.Native) != 26 || len(caps.Synthesized) != 0 || len(caps.Unsupported) != 0 {
		t.Fatalf("claude: native=%d synthesized=%d unsupported=%d", len(caps.Native), len(caps.Synthesized), len(caps.Unsupported))
	}
}

func TestIdentityOnlyHostsHaveNoCapabilities(t *testing.T) {
	for _, h := range axon.Hosts() {
		if h.Hooks {
			continue
		}
		if _, err := LoadCapabilities(h.Name); err == nil {
			t.Errorf("%s: LoadCapabilities returned nil error, want one (identity-only host)", h.Name)
		}
		if _, statErr := fs.Stat(axon.Spec(), "hosts/"+h.Name+"/capabilities.yaml"); statErr == nil {
			t.Errorf("%s: spec/hosts/%s/capabilities.yaml exists, want none", h.Name, h.Name)
		}
	}
}

func testCapabilities() Capabilities {
	return Capabilities{
		Host: "fake",
		Native: []Pair{
			{HostEvent: "pre_tool", Event: axon.Event("PreToolUse")},
		},
		Close: []Pair{
			{HostEvent: "notify", Event: axon.Event("Notification"), Note: "best effort"},
		},
		Synthesized: map[axon.Event]Recipe{
			axon.Event("Stop"): {Technique: "poll", Cost: "one extra process"},
		},
		Unsupported: []axon.Event{
			axon.Event("TeammateIdle"),
		},
	}
}

func TestCapabilitiesLevel(t *testing.T) {
	c := testCapabilities()

	tests := []struct {
		name      string
		event     axon.Event
		wantLevel Level
		wantOK    bool
	}{
		{"native", axon.Event("PreToolUse"), LevelNative, true},
		{"close", axon.Event("Notification"), LevelClose, true},
		{"synthesized", axon.Event("Stop"), LevelSynthesized, true},
		{"unsupported", axon.Event("TeammateIdle"), LevelUnsupported, true},
		{"unclassified", axon.Event("NoSuchEvent"), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLevel, gotOK := c.Level(tt.event)
			if gotLevel != tt.wantLevel || gotOK != tt.wantOK {
				t.Fatalf("Level(%q) = (%q, %v), want (%q, %v)", tt.event, gotLevel, gotOK, tt.wantLevel, tt.wantOK)
			}
		})
	}
}

func TestCapabilitiesRecipe(t *testing.T) {
	c := testCapabilities()

	if r, ok := c.Recipe(axon.Event("Stop")); !ok || r.Technique != "poll" {
		t.Fatalf("Recipe(Stop) = (%+v, %v), want technique=poll, ok=true", r, ok)
	}
	if _, ok := c.Recipe(axon.Event("PreToolUse")); ok {
		t.Fatal("Recipe(PreToolUse) ok, want false: it is native, not synthesized")
	}
	if _, ok := c.Recipe(axon.Event("NoSuchEvent")); ok {
		t.Fatal("Recipe(NoSuchEvent) ok, want false")
	}
}

func TestCapabilitiesHostEvent(t *testing.T) {
	c := testCapabilities()

	if he, ok := c.HostEvent(axon.Event("PreToolUse")); !ok || he != "pre_tool" {
		t.Fatalf("HostEvent(PreToolUse) = (%q, %v), want (pre_tool, true)", he, ok)
	}
	if he, ok := c.HostEvent(axon.Event("Notification")); !ok || he != "notify" {
		t.Fatalf("HostEvent(Notification) = (%q, %v), want (notify, true)", he, ok)
	}
	if _, ok := c.HostEvent(axon.Event("Stop")); ok {
		t.Fatal("HostEvent(Stop) ok, want false: it is synthesized, not native/close")
	}
	if _, ok := c.HostEvent(axon.Event("NoSuchEvent")); ok {
		t.Fatal("HostEvent(NoSuchEvent) ok, want false")
	}
}

// TestDecodeMapNativeWinsOverClose pins the decode-map precedence rule:
// a host event listed under both native: and close: decodes to the native
// row's canonical event, while a close row no native row claims still
// decodes (it is unambiguous).
func TestDecodeMapNativeWinsOverClose(t *testing.T) {
	c := Capabilities{
		Host:   "test",
		Native: []Pair{{HostEvent: "SessionEnd", Event: axon.EventSessionEnd}},
		Close: []Pair{
			{HostEvent: "SessionEnd", Event: axon.EventStop},
			{HostEvent: "BeforeModel", Event: axon.EventUserPromptSubmit},
		},
	}
	m, err := c.DecodeMap()
	if err != nil {
		t.Fatalf("DecodeMap: %v", err)
	}
	if got := m["SessionEnd"]; got != axon.EventSessionEnd {
		t.Errorf("SessionEnd decodes to %q, want %q (native must win)", got, axon.EventSessionEnd)
	}
	if got := m["BeforeModel"]; got != axon.EventUserPromptSubmit {
		t.Errorf("BeforeModel decodes to %q, want %q (close-only rows still decode)", got, axon.EventUserPromptSubmit)
	}
}

// TestDecodeMapRejectsDuplicateHostEvents: two rows in one section
// claiming the same host event are unresolvable on decode, so DecodeMap
// errors instead of silently keeping the last one.
func TestDecodeMapRejectsDuplicateHostEvents(t *testing.T) {
	for _, tc := range []struct {
		name string
		caps Capabilities
	}{
		{"native", Capabilities{Host: "test", Native: []Pair{
			{HostEvent: "X", Event: axon.EventStop},
			{HostEvent: "X", Event: axon.EventSessionEnd},
		}}},
		{"close", Capabilities{Host: "test", Close: []Pair{
			{HostEvent: "X", Event: axon.EventStop},
			{HostEvent: "X", Event: axon.EventSessionEnd},
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.caps.DecodeMap(); err == nil {
				t.Fatalf("DecodeMap accepted a duplicate host event under %s:, want an error", tc.name)
			}
		})
	}
}

// TestRegisteredCodecCapabilitiesBuildDecodeMap: every registered codec's
// capability file must survive DecodeMap, so a duplicate host event added
// to a real spec file fails the suite rather than a codec's New() panic in
// a consumer's process.
func TestRegisteredCodecCapabilitiesBuildDecodeMap(t *testing.T) {
	for _, name := range Registered() {
		c, _ := For(name)
		if _, err := c.Capabilities().DecodeMap(); err != nil {
			t.Errorf("%s: DecodeMap: %v", name, err)
		}
	}
}
