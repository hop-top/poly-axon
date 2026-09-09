package capture

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"hop.top/axon"
	"hop.top/axon/hooks"
)

// Status is where one host/event pair stands on the road from "the
// capability row claims this event exists" to "a fixture records what it
// looks like".
type Status string

const (
	// StatusFixture: spec/fixtures/hosts/<host>/<Event>.input.json exists.
	// Nothing to do.
	StatusFixture Status = "fixture"
	// StatusCaptured: no committed fixture, but a normalized capture is
	// sitting in the capture directory waiting to be reviewed and promoted.
	StatusCaptured Status = "captured"
	// StatusMissing: neither. This is a pair `axon fixture` refuses,
	// because emitting an envelope for it would mean inventing one.
	StatusMissing Status = "missing"
)

// Pair is one host/event row of the coverage report.
type Pair struct {
	Host string
	// Event is the canonical event name.
	Event axon.Event
	// HostEvent is the host-side name the envelope's discriminator
	// actually carries, which is what an operator configures the host
	// with. It differs from Event wherever the capability file says so
	// (gemini's BeforeTool for PreToolUse, opencode's dotted names).
	HostEvent string
	// Level is "native" or "close" from the host's capabilities.yaml.
	Level hooks.Level
	// Note carries the capability row's own caveat, where it has one —
	// gemini's "fires per model call, not per user prompt" being the one
	// that matters most to a capture.
	Note string
	// Blocking reports whether the canonical event blocks the host,
	// i.e. whether a wrong handler response would stall the CLI.
	Blocking bool
	// Status is where the pair stands.
	Status Status
	// SchemaPath and FixturePath are spec-root-relative, for printing.
	SchemaPath  string
	FixturePath string
	// HasSchema reports whether the input schema exists. A pair can have a
	// schema and no fixture (gemini Stop shares SessionEnd's shape), which
	// is why this is separate from Status.
	HasSchema bool
	// CapturePath is the file a capture would land in, absolute, when a
	// capture directory was given.
	CapturePath string
}

// Refused reports whether `axon fixture <host> <event>` refuses this pair
// today. That refusal is exactly "no input schema", which is the condition
// this harness exists to clear.
func (p Pair) Refused() bool { return !p.HasSchema }

// Coverage walks the spec tree and the capture directory and reports every
// host/event pair a registered codec can be invoked for.
//
// Nothing here is hardcoded: the pairs come from each host's
// capabilities.yaml, the fixture and schema statuses from the presence of
// the files themselves, exactly as docs/spec-contract-notes.md §4 requires
// ("directory presence is not implied by host.yaml's hooks: field"). A row
// therefore cannot drift from the tree — adding a fixture changes this
// report with no edit here.
//
// captureDir may be empty, in which case no pair is reported as captured.
func Coverage(captureDir string) ([]Pair, error) {
	blocking := map[axon.Event]bool{}
	for _, e := range axon.Events() {
		blocking[e.Name] = e.Blocking
	}
	var out []Pair
	for _, host := range hooks.Registered() {
		codec, ok := hooks.For(host)
		if !ok {
			return nil, fmt.Errorf("capture: no codec for %q", host)
		}
		caps := codec.Capabilities()
		rows := append(levelled(caps.Native, hooks.LevelNative), levelled(caps.Close, hooks.LevelClose)...)
		for _, r := range rows {
			p := Pair{
				Host:        host,
				Event:       r.pair.Event,
				HostEvent:   r.pair.HostEvent,
				Level:       r.level,
				Note:        r.pair.Note,
				Blocking:    blocking[r.pair.Event],
				SchemaPath:  fmt.Sprintf("hosts/%s/hooks/%s.input.schema.json", host, r.pair.Event),
				FixturePath: fmt.Sprintf("fixtures/hosts/%s/%s.input.json", host, r.pair.Event),
			}
			_, err := fs.Stat(axon.Spec(), p.SchemaPath)
			p.HasSchema = err == nil
			if captureDir != "" {
				p.CapturePath = CapturePath(captureDir, host, r.pair.Event)
			}
			p.Status = statusOf(p, captureDir)
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		return out[i].Event < out[j].Event
	})
	return out, nil
}

type levelledPair struct {
	pair  hooks.Pair
	level hooks.Level
}

func levelled(pairs []hooks.Pair, lvl hooks.Level) []levelledPair {
	out := make([]levelledPair, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, levelledPair{pair: p, level: lvl})
	}
	return out
}

func statusOf(p Pair, captureDir string) Status {
	if _, err := fs.Stat(axon.Spec(), p.FixturePath); err == nil {
		return StatusFixture
	}
	if captureDir != "" {
		if _, err := os.Stat(p.CapturePath); err == nil {
			return StatusCaptured
		}
	}
	return StatusMissing
}

// CapturePath is where a recorded envelope for one pair lands: one file
// per host/event, named the way the fixture it may become is named, so
// promoting a capture is a copy with no rename to get wrong.
func CapturePath(dir, host string, ev axon.Event) string {
	return filepath.Join(dir, host, string(ev)+".input.json")
}

// Summarize counts the pairs by status.
func Summarize(pairs []Pair) (fixture, captured, missing int) {
	for _, p := range pairs {
		switch p.Status {
		case StatusFixture:
			fixture++
		case StatusCaptured:
			captured++
		case StatusMissing:
			missing++
		}
	}
	return
}

// NextStep is the one-line instruction printed next to a pair, so the
// report says what to do rather than only what is true.
func NextStep(p Pair) string {
	switch p.Status {
	case StatusFixture:
		return "done"
	case StatusCaptured:
		return "review " + p.CapturePath + ", then promote it to spec/" + p.FixturePath
	default:
		var b strings.Builder
		b.WriteString("run the host until it raises ")
		b.WriteString(p.HostEvent)
		if p.Note != "" {
			b.WriteString(" (")
			b.WriteString(p.Note)
			b.WriteString(")")
		}
		return b.String()
	}
}
