package hooks_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"strings"
	"testing"

	"hop.top/axon"
	"hop.top/axon/hooks"
	_ "hop.top/axon/hooks/hosts"
)

// Check 1: every hooked host in the spec has a codec, and vice versa.
func TestEveryHookedHostHasCodec(t *testing.T) {
	want := map[string]bool{}
	for _, h := range axon.HookedHosts() {
		want[h.Name] = true
	}
	for _, name := range hooks.Registered() {
		if !want[name] {
			t.Errorf("codec %q registered but spec/hosts/%s/host.yaml has hooks: false or is missing", name, name)
		}
		delete(want, name)
	}
	for name := range want {
		// aider, goose, vibe, openhands are hooked in the spec but have no codec yet (design §3):
		// they must be marked hooks: false until a codec lands, or listed here explicitly.
		if !codecDeferred[name] {
			t.Errorf("spec/hosts/%s has hooks: true but no codec is registered", name)
		}
	}
}

// codecDeferred names hosts whose capability maps ship before their codec.
// Removing a name here is the "codec landed" signal.
var codecDeferred = map[string]bool{"aider": true, "goose": true, "vibe": true, "openhands": true}

// Check 2 lives in hooks/capabilities_test.go (TestEveryNativeEventClassifiedOnce).

// Check 5 lives in registry_test.go (TestAliasUniqueness): no two hosts
// claim the same alias, and no alias collides with a canonical name.

// Check 3 + 4: fixtures round-trip and validate; a codec with zero fixtures fails.
func TestFixturesRoundTripAndValidate(t *testing.T) {
	for _, name := range hooks.Registered() {
		c, _ := hooks.For(name)
		dir := path.Join("fixtures", "hosts", name)
		entries, err := fs.ReadDir(axon.Spec(), dir)
		if err != nil || len(entries) == 0 {
			t.Errorf("%s: no fixtures under spec/%s", name, dir)
			continue
		}
		for _, e := range entries {
			raw, _ := fs.ReadFile(axon.Spec(), path.Join(dir, e.Name()))
			parts := strings.Split(strings.TrimSuffix(e.Name(), ".json"), ".")
			ev := axon.Event(parts[0])
			switch {
			case len(parts) == 2 && parts[1] == "input":
				in, err := c.DecodeInput(raw)
				if err != nil {
					t.Errorf("%s/%s: DecodeInput: %v", name, e.Name(), err)
					continue
				}
				again, err := c.EncodeInput(in)
				if err != nil {
					t.Errorf("%s/%s: EncodeInput: %v", name, e.Name(), err)
					continue
				}
				if !sameJSON(raw, again) {
					t.Errorf("%s/%s: round trip differs\nwant %s\ngot  %s", name, e.Name(), raw, again)
				}
				if err := hooks.ValidateInput(name, ev, again); err != nil {
					t.Errorf("%s/%s: %v", name, e.Name(), err)
				}
			case len(parts) == 2:
				action := hooks.Action(parts[1])
				exitCode := exitFor(t, name, action)
				d, err := c.DecodeDecision(raw, exitCode)
				if err != nil {
					t.Errorf("%s/%s: DecodeDecision: %v", name, e.Name(), err)
					continue
				}
				if d.Action != action {
					t.Errorf("%s/%s: decoded action %q, fixture says %q", name, e.Name(), d.Action, action)
				}
				if d.Event != "" && d.Event != ev {
					t.Errorf("%s/%s: decoded event %q, fixture says %q", name, e.Name(), d.Event, ev)
				}
				// The fixture's file name carries the event; set it before
				// re-encoding so codecs whose wire shape varies by event (e.g.
				// claude, codex) re-encode the right shape rather than
				// whatever blank-Event default they fall back to.
				d.Event = ev
				again, code, err := c.EncodeDecision(d)
				if err != nil || code != exitCode {
					t.Errorf("%s/%s: EncodeDecision exit %d err %v; want %d", name, e.Name(), code, err, exitCode)
					continue
				}
				if len(again) > 0 {
					if err := hooks.ValidateDecision(name, ev, action, again); err != nil {
						t.Errorf("%s/%s: %v", name, e.Name(), err)
					}
				}
			default:
				t.Errorf("%s/%s: fixture name must be <Event>.input.json or <Event>.<action>.json", name, e.Name())
			}
		}
	}
}

// Check 6: no kit dependency.
func TestGoModHasNoKit(t *testing.T) {
	b, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "hop.top/kit") {
		t.Fatal("go.mod requires hop.top/kit; axon must never import kit (design §4)")
	}
}

func exitFor(t *testing.T, host string, a hooks.Action) int {
	h, _ := axon.Get(host)
	switch a {
	case hooks.ActionAllow:
		return h.ExitCodes.Allow
	case hooks.ActionWarn:
		return h.ExitCodes.Warn
	case hooks.ActionBlock:
		return h.ExitCodes.Block
	case hooks.ActionRewrite:
		return h.ExitCodes.Rewrite
	}
	t.Fatalf("unknown action %q", a)
	return 0
}

func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	xa, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return string(xa) == string(yb)
}

// TestDecodeUsesNativeRowsOnly is the cross-codec form of the same policy
// the gemini codec's own test pins: a host event listed under close: is a
// lossy approximation used to ENCODE a canonical event into a host name,
// never to DECODE a host event back into a canonical one. Whenever a host
// event appears under both native: and close:, decoding it must yield the
// native row's canonical event.
func TestDecodeUsesNativeRowsOnly(t *testing.T) {
	for _, name := range hooks.Registered() {
		c, _ := hooks.For(name)
		caps := c.Capabilities()
		native := map[string]axon.Event{}
		for _, p := range caps.Native {
			native[p.HostEvent] = p.Event
		}
		for _, p := range caps.Close {
			want, dup := native[p.HostEvent]
			if !dup {
				continue
			}
			raw, err := c.EncodeInput(hooks.Input{Event: want, SessionID: "s", Cwd: "/tmp"})
			if err != nil {
				t.Errorf("%s: EncodeInput(%s): %v", name, want, err)
				continue
			}
			in, err := c.DecodeInput(raw)
			if err != nil {
				t.Errorf("%s: DecodeInput(%s): %v", name, raw, err)
				continue
			}
			if in.Event != want {
				t.Errorf("%s: host event %q decodes to %q; close rows must never win a decode, want native %q",
					name, p.HostEvent, in.Event, want)
			}
		}
	}
}

// TestEncodeDecisionRejectsUnknownAction: a Decision carrying an Action
// outside the four canonical constants (including the zero value) is a
// programming error in the caller, and every codec must say so. Emitting
// a default shape — opencode's "block", claude/codex's empty
// permissionDecision — is the silent fallback the design forbids.
func TestEncodeDecisionRejectsUnknownAction(t *testing.T) {
	for _, name := range hooks.Registered() {
		c, _ := hooks.For(name)
		for _, a := range []hooks.Action{"", "nonsense", "deny"} {
			t.Run(name+"/"+string(a), func(t *testing.T) {
				out, _, err := c.EncodeDecision(hooks.Decision{Action: a})
				if err == nil {
					t.Fatalf("EncodeDecision(action %q) returned %s with nil error; want ErrUnsupportedAction", a, out)
				}
				if !errors.Is(err, hooks.ErrUnsupportedAction) {
					t.Fatalf("EncodeDecision(action %q) error = %v; want ErrUnsupportedAction", a, err)
				}
			})
		}
	}
}

// TestEncodeDecisionRejectsUnsupportedEvent: a decision naming an event
// the host's capability file marks unsupported (or does not classify at
// all) has no wire shape, so it must error rather than be emitted in the
// host's default shape. A blank Event keeps the default shape, per
// Decision.Event's documented contract.
func TestEncodeDecisionRejectsUnsupportedEvent(t *testing.T) {
	for _, name := range hooks.Registered() {
		c, _ := hooks.For(name)
		caps := c.Capabilities()
		for _, ev := range append([]axon.Event{"NotAnEvent"}, caps.Unsupported...) {
			t.Run(name+"/"+string(ev), func(t *testing.T) {
				out, _, err := c.EncodeDecision(hooks.Decision{Action: hooks.ActionBlock, Event: ev, Message: "no"})
				if err == nil {
					t.Fatalf("EncodeDecision(event %q) returned %s with nil error; want ErrUnsupportedEvent", ev, out)
				}
				if !errors.Is(err, hooks.ErrUnsupportedEvent) {
					t.Fatalf("EncodeDecision(event %q) error = %v; want ErrUnsupportedEvent", ev, err)
				}
			})
		}
	}
}

// TestEncodeInputRequiresNativeOrClose: EncodeInput builds a payload the
// host itself would have emitted, so only events the capability file
// marks native or close have one. A synthesized event is derived by the
// runtime from some other host event and has no envelope of its own;
// encoding it as if the host raised it is the silent fallback the design
// forbids.
func TestEncodeInputRequiresNativeOrClose(t *testing.T) {
	for _, name := range hooks.Registered() {
		c, _ := hooks.For(name)
		caps := c.Capabilities()
		evs := []axon.Event{"NotAnEvent"}
		evs = append(evs, caps.Unsupported...)
		for ev := range caps.Synthesized {
			evs = append(evs, ev)
		}
		for _, ev := range evs {
			t.Run(name+"/"+string(ev), func(t *testing.T) {
				out, err := c.EncodeInput(hooks.Input{Event: ev, SessionID: "s", Cwd: "/tmp"})
				if err == nil {
					t.Fatalf("EncodeInput(%q) returned %s with nil error; want ErrUnsupportedEvent", ev, out)
				}
				if !errors.Is(err, hooks.ErrUnsupportedEvent) {
					t.Fatalf("EncodeInput(%q) error = %v; want ErrUnsupportedEvent", ev, err)
				}
			})
		}
	}
}

// TestDecodeInputRejectsMissingAndUnknownEvent: the envelope's event field
// is the discriminator, so a missing one is a malformed payload (ErrSchema)
// and an unrecognized one is an event this host does not speak
// (ErrUnsupportedEvent). Neither may yield an Input with a bogus Event and
// a nil error.
func TestDecodeInputRejectsMissingAndUnknownEvent(t *testing.T) {
	for _, name := range hooks.Registered() {
		c, _ := hooks.For(name)
		disc := "hook_event_name"
		if h, ok := axon.Get(name); ok && h.EnvelopeDiscriminator != "" {
			disc = h.EnvelopeDiscriminator
		}
		t.Run(name+"/missing", func(t *testing.T) {
			in, err := c.DecodeInput([]byte(`{"session_id":"s"}`))
			if err == nil {
				t.Fatalf("DecodeInput with no %s returned Event %q with nil error; want ErrSchema", disc, in.Event)
			}
			if !errors.Is(err, hooks.ErrSchema) {
				t.Fatalf("DecodeInput with no %s: error = %v; want ErrSchema", disc, err)
			}
		})
		t.Run(name+"/unknown", func(t *testing.T) {
			raw := []byte(`{"` + disc + `":"Nonsense","session_id":"s"}`)
			in, err := c.DecodeInput(raw)
			if err == nil {
				t.Fatalf("DecodeInput(%s) returned Event %q with nil error; want an error", raw, in.Event)
			}
			if !errors.Is(err, hooks.ErrUnsupportedEvent) {
				t.Fatalf("DecodeInput(%s): error = %v; want ErrUnsupportedEvent", raw, err)
			}
		})
	}
}

// Check 7: every event a capability file marks native for a REGISTERED
// codec has a golden input fixture, or is listed in fixtureDeferred with
// a reason. Fixtures are what prove a codec round-trips the shapes the
// host actually sends; a native event with no fixture is an untested
// wire path. This is the check that would have caught the gemini
// SessionEnd decode collision.
func TestEveryNativeEventHasInputFixture(t *testing.T) {
	for _, name := range hooks.Registered() {
		deferred := map[axon.Event]bool{}
		for _, ev := range fixtureDeferred[name] {
			deferred[ev] = true
		}
		have := map[axon.Event]bool{}
		entries, err := fs.ReadDir(axon.Spec(), path.Join("fixtures", "hosts", name))
		if err != nil {
			t.Errorf("%s: no fixture directory: %v", name, err)
			continue
		}
		for _, e := range entries {
			if parts := strings.Split(e.Name(), "."); len(parts) == 3 && parts[1] == "input" {
				have[axon.Event(parts[0])] = true
			}
		}
		c, _ := hooks.For(name)
		for _, p := range c.Capabilities().Native {
			switch {
			case have[p.Event]:
				if deferred[p.Event] {
					t.Errorf("%s: %s is in fixtureDeferred but spec/fixtures/hosts/%s/%s.input.json exists; drop the deferral",
						name, p.Event, name, p.Event)
				}
			case deferred[p.Event]:
				// Explicitly deferred; see fixtureDeferred's comment.
			default:
				t.Errorf("%s: native event %s has no spec/fixtures/hosts/%s/%s.input.json and is not in fixtureDeferred",
					name, p.Event, name, p.Event)
			}
		}
	}
}

// fixtureDeferred names native events that ship without an input fixture,
// per host. Same pattern and same contract as codecDeferred: an entry is
// a promise, not a permission, and removing one is the "fixture landed"
// signal. Every entry below is a native mapping seeded from nerv's
// adapter tables for which no golden envelope has been captured from the
// host yet — the shapes are unverified, so writing a fixture from the
// schema alone would only test axon against itself.
//
// codex is absent on purpose: all three of its native events have
// fixtures, so it has nothing to defer.
var fixtureDeferred = map[string][]axon.Event{
	// claude is the reference host: its capability file marks all 26
	// canonical events native, but only the six tool-gate and session
	// events below have captured envelopes. The other 20 are event names
	// nerv's adapter table asserts claude raises, not shapes anyone has
	// recorded.
	"claude": {
		axon.EventSessionEnd,
		axon.EventPostToolUseFailure,
		axon.EventPermissionDenied,
		axon.EventNotification,
		axon.EventSubagentStart,
		axon.EventSubagentStop,
		axon.EventTaskCreated,
		axon.EventTaskCompleted,
		axon.EventStopFailure,
		axon.EventTeammateIdle,
		axon.EventInstructionsLoaded,
		axon.EventConfigChange,
		axon.EventCwdChanged,
		axon.EventFileChanged,
		axon.EventWorktreeCreate,
		axon.EventWorktreeRemove,
		axon.EventPreCompact,
		axon.EventPostCompact,
		axon.EventElicitation,
		axon.EventElicitationResult,
	},
	// gemini: the four host events below (BeforeAgent, AfterAgent,
	// Notification, PreCompress) are in nerv's map but no envelope has
	// been captured for them.
	"gemini": {
		axon.EventSubagentStart,
		axon.EventSubagentStop,
		axon.EventNotification,
		axon.EventPreCompact,
	},
	// opencode: the five session-lifecycle events below (session.deleted,
	// session.idle, session.error, session.compacted, permission.replied)
	// have no captured plugin-callback payload.
	"opencode": {
		axon.EventSessionEnd,
		axon.EventStop,
		axon.EventStopFailure,
		axon.EventPostCompact,
		axon.EventPermissionDenied,
	},
}

// TestEncodeInputExtraNeverOverwritesCanonicalFields: Extra carries the
// host-specific keys the canonical Input does not name, so it is written
// first and every canonical field overwrites a colliding key. The reverse
// order let Extra clobber the envelope's own event discriminator and
// session id — EncodeInput would emit an envelope naming an event other
// than in.Event, with a nil error.
func TestEncodeInputExtraNeverOverwritesCanonicalFields(t *testing.T) {
	for _, name := range hooks.Registered() {
		c, _ := hooks.For(name)
		disc := "hook_event_name"
		if h, ok := axon.Get(name); ok && h.EnvelopeDiscriminator != "" {
			disc = h.EnvelopeDiscriminator
		}
		t.Run(name, func(t *testing.T) {
			raw, err := c.EncodeInput(hooks.Input{
				Event: axon.EventPreToolUse, SessionID: "real", Cwd: "/real",
				Extra: map[string]any{disc: "CLOBBER", "session_id": "CLOBBER"},
			})
			if err != nil {
				t.Fatalf("EncodeInput: %v", err)
			}
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatalf("unmarshal %s: %v", raw, err)
			}
			if m[disc] == "CLOBBER" {
				t.Errorf("Extra overwrote the event discriminator %s: %s", disc, raw)
			}
			if m["session_id"] == "CLOBBER" {
				t.Errorf("Extra overwrote session_id: %s", raw)
			}
			// The envelope must still decode back to the event it was
			// asked to encode.
			in, err := c.DecodeInput(raw)
			if err != nil {
				t.Fatalf("DecodeInput(%s): %v", raw, err)
			}
			if in.Event != axon.EventPreToolUse {
				t.Errorf("round trip yielded event %q, want %q: %s", in.Event, axon.EventPreToolUse, raw)
			}
		})
	}
}
