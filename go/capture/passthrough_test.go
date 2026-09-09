package capture_test

import (
	"io/fs"
	"testing"

	"hop.top/axon"
	"hop.top/axon/capture"
	"hop.top/axon/hooks"
	_ "hop.top/axon/hooks/hosts"
)

// TestPassThroughDecodesToAllow is the check that proves a capture handler
// cannot break the operator's CLI. For every host with a codec and every
// event that host raises, the response the handler would emit is fed back
// through the host's own DecodeDecision — the same function the runtime
// uses to read a handler's answer — and must come back as ActionAllow.
//
// Anything else is a handler that silently denies a tool call, a prompt or
// a turn on a machine where it is installed only to watch.
func TestPassThroughDecodesToAllow(t *testing.T) {
	for _, host := range hooks.Registered() {
		codec, _ := hooks.For(host)
		for _, ev := range hostEvents(codec) {
			t.Run(host+"/"+string(ev), func(t *testing.T) {
				pt, err := capture.PassThroughFor(host, ev)
				if err != nil {
					t.Fatalf("PassThroughFor: %v", err)
				}
				d, err := codec.DecodeDecision(pt.Stdout, pt.Exit)
				if err != nil {
					t.Fatalf("DecodeDecision(%q, %d): %v", pt.Stdout, pt.Exit, err)
				}
				if d.Action != hooks.ActionAllow {
					t.Fatalf("pass-through %q at exit %d decodes to %q; a capture handler must never do anything but allow",
						pt.Stdout, pt.Exit, d.Action)
				}
			})
		}
	}
}

// TestPassThroughValidatesAgainstDecisionSchema checks the same responses
// against the committed allow schemas: where spec/hosts/<host>/hooks/
// ships <Event>.allow.schema.json, the bytes the handler emits must
// satisfy it. This is what ties the response to the spec rather than to
// the codec alone.
//
// Pairs with no allow schema are counted, not skipped silently, and the
// test fails if NO pair had one — that would mean the check had quietly
// stopped proving anything.
func TestPassThroughValidatesAgainstDecisionSchema(t *testing.T) {
	validated := 0
	for _, host := range hooks.Registered() {
		codec, _ := hooks.For(host)
		for _, ev := range hostEvents(codec) {
			if !hasAllowSchema(host, ev) {
				continue
			}
			t.Run(host+"/"+string(ev), func(t *testing.T) {
				pt, err := capture.PassThroughFor(host, ev)
				if err != nil {
					t.Fatalf("PassThroughFor: %v", err)
				}
				if len(pt.Stdout) == 0 {
					t.Fatalf("%s/%s ships an allow schema but the pass-through writes nothing; "+
						"an event with a decision channel must use it", host, ev)
				}
				if err := hooks.ValidateDecision(host, ev, hooks.ActionAllow, pt.Stdout); err != nil {
					t.Fatalf("pass-through %s fails the committed allow schema: %v", pt.Stdout, err)
				}
			})
			validated++
		}
	}
	if validated == 0 {
		t.Fatal("no host/event pair has a committed allow schema; this test proved nothing")
	}
}

// TestPassThroughCoversEveryBlockingEvent: a non-blocking event's handler
// can only be late, but a blocking one's handler decides whether the host
// proceeds. Every blocking canonical event a registered host raises must
// therefore have a derivable pass-through, with no error and no gap.
func TestPassThroughCoversEveryBlockingEvent(t *testing.T) {
	blocking := map[axon.Event]bool{}
	for _, e := range axon.Events() {
		if e.Blocking {
			blocking[e.Name] = true
		}
	}
	if len(blocking) == 0 {
		t.Fatal("spec/events.yaml lists no blocking events; this test proved nothing")
	}
	covered := 0
	for _, host := range hooks.Registered() {
		codec, _ := hooks.For(host)
		for _, ev := range hostEvents(codec) {
			if !blocking[ev] {
				continue
			}
			t.Run(host+"/"+string(ev), func(t *testing.T) {
				pt, err := capture.PassThroughFor(host, ev)
				if err != nil {
					t.Fatalf("blocking event has no pass-through: %v", err)
				}
				h, _ := axon.Get(host)
				if pt.Exit != h.ExitCodes.Allow {
					t.Fatalf("pass-through exits %d; host.yaml's allow code is %d, and any other value blocks",
						pt.Exit, h.ExitCodes.Allow)
				}
				if pt.Exit == h.ExitCodes.Block && h.ExitCodes.Block != h.ExitCodes.Allow {
					t.Fatalf("pass-through exits with the host's BLOCK code %d", pt.Exit)
				}
			})
			covered++
		}
	}
	if covered == 0 {
		t.Fatal("no registered host raises a blocking event; this test proved nothing")
	}
	t.Logf("checked %d blocking host/event pairs", covered)
}

// TestPassThroughSourceIsReported: `axon capture plan` prints the
// derivation next to the response so an operator can check the reasoning
// rather than trust it. A blank Source would print an unexplained shape.
func TestPassThroughSourceIsReported(t *testing.T) {
	for _, host := range hooks.Registered() {
		codec, _ := hooks.For(host)
		for _, ev := range hostEvents(codec) {
			pt, err := capture.PassThroughFor(host, ev)
			if err != nil {
				t.Fatalf("%s/%s: %v", host, ev, err)
			}
			switch pt.Source {
			case capture.SourceCodec, capture.SourceSilent:
			default:
				t.Errorf("%s/%s: Source = %q, want one of the two documented derivations", host, ev, pt.Source)
			}
			if pt.Source == capture.SourceSilent && len(pt.Stdout) != 0 {
				t.Errorf("%s/%s: Source says silence but Stdout is %q", host, ev, pt.Stdout)
			}
		}
	}
}

// TestPassThroughRejectsUnknownHostAndEvent: a pair the host does not
// raise has no pass-through, and saying so beats emitting a default shape
// for an event the host never sends.
func TestPassThroughRejectsUnknownHostAndEvent(t *testing.T) {
	if _, err := capture.PassThroughFor("not-a-host", axon.EventStop); err == nil {
		t.Error("PassThroughFor with an unknown host returned nil error")
	}
	if _, err := capture.PassThroughFor("aider", axon.EventStop); err == nil {
		t.Error("PassThroughFor for a host with no codec returned nil error")
	}
	// gemini's capabilities.yaml marks TeammateIdle unsupported.
	if _, err := capture.PassThroughFor(axon.HostGemini, axon.EventTeammateIdle); err == nil {
		t.Error("PassThroughFor for an unsupported event returned nil error")
	}
}

// hostEvents returns every canonical event a codec's capability file marks
// native or close — the set a hook handler can actually be invoked for.
func hostEvents(c hooks.Codec) []axon.Event {
	caps := c.Capabilities()
	out := make([]axon.Event, 0, len(caps.Native)+len(caps.Close))
	seen := map[axon.Event]bool{}
	for _, p := range append(append([]hooks.Pair{}, caps.Native...), caps.Close...) {
		if !seen[p.Event] {
			seen[p.Event] = true
			out = append(out, p.Event)
		}
	}
	return out
}

func hasAllowSchema(host string, ev axon.Event) bool {
	_, err := fs.Stat(axon.Spec(), "hosts/"+host+"/hooks/"+string(ev)+".allow.schema.json")
	return err == nil
}
