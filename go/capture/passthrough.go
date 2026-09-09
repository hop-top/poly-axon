package capture

import (
	"errors"
	"fmt"

	"hop.top/axon"
	"hop.top/axon/hooks"
)

// PassThrough is the response a capture handler writes back to the host
// after recording an envelope: the "carry on, I decided nothing" answer in
// that host's own wire convention.
//
// Getting this wrong breaks the operator's CLI — a blocking event whose
// hook returns a deny-shaped body or a non-zero exit stops a tool call, a
// prompt, or a turn. So it is never written by hand here. Both fields come
// from the host's own codec, which is the same code path the conformance
// suite round-trips against the committed decision fixtures.
type PassThrough struct {
	// Stdout is what the handler writes to standard output. Empty means
	// write nothing at all, which every registered codec's DecodeDecision
	// classifies as allow.
	Stdout []byte
	// Exit is the process exit status the handler ends with.
	Exit int
	// Source records which of the two derivations below produced this
	// response, so `axon capture plan` can show its work rather than
	// asking the operator to take it on faith.
	Source string
}

// Derivations reported in PassThrough.Source.
const (
	// SourceCodec: the host's codec encoded an explicit allow decision for
	// this event, and (where the spec ships an allow schema for the pair)
	// hooks.ValidateDecision accepts it.
	SourceCodec = "codec allow decision"
	// SourceSilent: the host's codec reports the event has no decision
	// channel at all (ErrUnsupportedEvent from EncodeDecision), so the
	// handler stays silent and exits with the host's allow code. Every
	// registered codec decodes empty stdout at the allow exit code back to
	// ActionAllow, which is what makes silence the safe answer rather than
	// merely the quiet one.
	SourceSilent = "silence at the host's allow exit code"
)

// PassThroughFor returns the response a capture handler must emit for one
// host event.
//
// The derivation, in order:
//
//  0. Reject a pair the host's capability file does not mark native or
//     close. The host never raises it, so there is nothing to capture and
//     no handler to install.
//
//  1. Ask the host's codec to encode Decision{Action: allow, Event: ev}.
//     A codec that produces one has a documented allow shape for the event
//     — claude's `{}`, gemini's {"decision":"allow"}, opencode's
//     {"action":"allow"} — and that shape, with the exit code the codec
//     returns alongside it, is the answer.
//
//  2. A codec that returns ErrUnsupportedEvent is stating that the host's
//     hook contract has no decision channel for this event: claude's codec
//     does exactly this for 21 of its 26 native events, since Claude only
//     reads a decision back from PreToolUse, PermissionRequest,
//     PostToolUse and SessionStart. For those, the handler writes nothing
//     and exits with the host's own allow code from host.yaml
//     (axon.Host.ExitCodes.Allow), which every codec's DecodeDecision
//     turns back into ActionAllow.
//
// Any other codec error is returned: it means the pair is not one this
// host raises at all, and a handler should not be installed for it.
func PassThroughFor(host string, ev axon.Event) (PassThrough, error) {
	h, ok := axon.Get(host)
	if !ok {
		return PassThrough{}, fmt.Errorf("capture: unknown host %q", host)
	}
	codec, ok := hooks.For(host)
	if !ok {
		return PassThrough{}, fmt.Errorf("capture: no codec registered for host %q", host)
	}
	// Gate on the capability file BEFORE asking the codec, because both a
	// pair the host never raises and a pair with no decision channel come
	// back from EncodeDecision as ErrUnsupportedEvent. Only the second is a
	// pass-through; treating the first as one would hand the operator a
	// handler configured for an event the host does not send.
	if lvl, classified := codec.Capabilities().Level(ev); !classified ||
		(lvl != hooks.LevelNative && lvl != hooks.LevelClose) {
		return PassThrough{}, fmt.Errorf("capture: %s does not raise %s: %w", host, ev, hooks.ErrUnsupportedEvent)
	}
	out, exit, err := codec.EncodeDecision(hooks.Decision{Action: hooks.ActionAllow, Event: ev})
	switch {
	case err == nil:
		return PassThrough{Stdout: out, Exit: exit, Source: SourceCodec}, nil
	case errors.Is(err, hooks.ErrUnsupportedEvent):
		// No decision channel for this event. Silence at the allow exit
		// code is the host's own "nothing to say" — see SourceSilent.
		return PassThrough{Stdout: nil, Exit: h.ExitCodes.Allow, Source: SourceSilent}, nil
	default:
		return PassThrough{}, fmt.Errorf("capture: %s/%s: %w", host, ev, err)
	}
}
