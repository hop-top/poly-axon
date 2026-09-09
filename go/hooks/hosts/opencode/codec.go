// Package opencode implements the axon hooks codec for OpenCode.
//
// OpenCode hooks are in-process JS/TS plugin callbacks, not subprocesses:
// there is no exit-code contract (nerv docs/research/opencode.md:14;
// spec/hosts/opencode/host.yaml omits exit_codes). EncodeDecision always
// returns exit 0. DecodeDecision ignores its exit argument and decodes the
// decision from stdout JSON only; empty stdout decodes to allow.
//
// Event names are never hardcoded here: they come from
// Capabilities().HostEvent for encoding and Capabilities().DecodeMap —
// where native rows win over close rows — for decoding, so
// spec/hosts/opencode/capabilities.yaml is the single source of the
// canonical<->dotted event mapping.
package opencode

import (
	"bytes"
	"encoding/json"
	"fmt"

	"hop.top/axon"
	"hop.top/axon/hooks"
)

type codec struct {
	host axon.Host
	caps hooks.Capabilities
	// rev maps a host-side dotted event name to its canonical event.
	// Native rows win over close rows (hooks.Capabilities.DecodeMap): a
	// close row is an encode-side approximation and never displaces a
	// native one.
	rev map[string]axon.Event
}

// New returns the OpenCode codec. Panics if the spec lacks the host or its
// capability file, which the conformance tests prevent.
func New() hooks.Codec {
	h, ok := axon.Get(axon.HostOpencode)
	if !ok {
		panic("axon: opencode host missing from spec")
	}
	caps, err := hooks.LoadCapabilities(axon.HostOpencode)
	if err != nil {
		panic(err)
	}
	rev, err := caps.DecodeMap()
	if err != nil {
		panic(err)
	}
	return &codec{host: h, caps: caps, rev: rev}
}

func (c *codec) Host() string                     { return c.host.Name }
func (c *codec) Capabilities() hooks.Capabilities { return c.caps }

func (c *codec) EncodeInput(in hooks.Input) ([]byte, error) {
	native, ok := c.caps.HostEvent(in.Event)
	if !ok {
		return nil, fmt.Errorf("%w: %s/%s", hooks.ErrUnsupportedEvent, c.host.Name, in.Event)
	}
	// Extra is written FIRST; the canonical fields below overwrite any
	// colliding key. Written last it clobbered the "type" discriminator
	// and session_id — see the claude codec for the same note.
	out := map[string]any{}
	for k, v := range in.Extra {
		out[k] = v
	}
	out["type"] = native
	out["session_id"] = in.SessionID
	switch in.Event {
	case axon.EventPreToolUse, axon.EventPermissionRequest:
		out["tool"], out["input"] = in.ToolName, in.ToolInput
	case axon.EventPostToolUse:
		out["tool"], out["input"], out["output"] = in.ToolName, in.ToolInput, in.ToolResponse
	case axon.EventUserPromptSubmit:
		out["prompt"] = in.Prompt
	}
	return json.Marshal(out)
}

func (c *codec) DecodeInput(raw []byte) (hooks.Input, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return hooks.Input{}, fmt.Errorf("%w: opencode input: %v", hooks.ErrSchema, err)
	}
	native, ok := m["type"].(string)
	if !ok || native == "" {
		// A missing discriminator is a malformed payload, not an
		// unsupported event: reporting it as "opencode/" named no event
		// at all.
		return hooks.Input{}, fmt.Errorf("%w: opencode input: missing type", hooks.ErrSchema)
	}
	ev, known := c.rev[native]
	if !known {
		return hooks.Input{}, fmt.Errorf("%w: opencode/%s", hooks.ErrUnsupportedEvent, native)
	}
	in := hooks.Input{Event: ev, Extra: map[string]any{}}
	in.SessionID, _ = m["session_id"].(string)
	in.ToolName, _ = m["tool"].(string)
	in.ToolInput, _ = m["input"].(map[string]any)
	in.ToolResponse, _ = m["output"].(map[string]any)
	in.Prompt, _ = m["prompt"].(string)
	for k, v := range m {
		switch k {
		case "type", "session_id", "tool", "input", "output", "prompt":
		default:
			in.Extra[k] = v
		}
	}
	return in, nil
}

// EncodeDecision always returns exit 0: OpenCode has no exit-code contract,
// the decision travels entirely in stdout JSON.
//
// Warn is passed through verbatim as {"action":"warn","message":...},
// mirroring nerv adapters/opencode/translate.go FromDecision, which has no
// warn branch at all: it just marshals string(d.Action) unconditionally,
// so a merger.Decision{Action: "warn"} already produces this exact shape
// on the wire today. Folding warn to block here would change semantics
// (block a tool the handler only warned about), so it is never done.
func (c *codec) EncodeDecision(d hooks.Decision) ([]byte, int, error) {
	if err := checkEvent(c.caps, d.Event); err != nil {
		return nil, 0, err
	}
	switch d.Action {
	case hooks.ActionAllow:
		b, err := json.Marshal(map[string]any{"action": "allow"})
		return b, 0, err
	case hooks.ActionRewrite:
		// Field name "rewrite" per xat spec/hook-output/opencode/
		// tool.execute.before-rewrite.json, which decision 1 says wins on
		// field names over the brief's "rewrite under input" sentence.
		b, err := json.Marshal(map[string]any{"action": "rewrite", "rewrite": d.Rewrite})
		return b, 0, err
	case hooks.ActionWarn:
		b, err := json.Marshal(map[string]any{"action": "warn", "message": d.Message})
		return b, 0, err
	case hooks.ActionBlock:
		b, err := json.Marshal(map[string]any{"action": "block", "message": d.Message})
		return b, 0, err
	default:
		// Exhaustive over the four canonical actions. Anything else —
		// including the zero Action — used to fall through to "block",
		// turning a caller's bug into a silent tool denial.
		return nil, 0, fmt.Errorf("%w: opencode cannot express action %q", hooks.ErrUnsupportedAction, d.Action)
	}
}

// checkEvent rejects a decision naming an event OpenCode has no wire
// shape for. A blank Event keeps the host's default shape, per
// hooks.Decision.Event's contract; anything the capability file does not
// mark native or close is ErrUnsupportedEvent, never a decision emitted
// for an event the host never raises.
func checkEvent(caps hooks.Capabilities, ev axon.Event) error {
	if ev == "" {
		return nil
	}
	lvl, ok := caps.Level(ev)
	if !ok || (lvl != hooks.LevelNative && lvl != hooks.LevelClose) {
		return fmt.Errorf("%w: opencode/%s", hooks.ErrUnsupportedEvent, ev)
	}
	return nil
}

// DecodeDecision ignores exit: OpenCode decisions live in stdout JSON only.
// Empty stdout decodes to allow.
func (c *codec) DecodeDecision(stdout []byte, _ int) (hooks.Decision, error) {
	if len(bytes.TrimSpace(stdout)) == 0 {
		return hooks.Decision{Action: hooks.ActionAllow}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(stdout, &m); err != nil {
		return hooks.Decision{}, fmt.Errorf("%w: opencode decision: %v", hooks.ErrSchema, err)
	}
	d := hooks.Decision{Action: hooks.ActionAllow}
	action, _ := m["action"].(string)
	switch action {
	case "block":
		d.Action = hooks.ActionBlock
	case "warn":
		d.Action = hooks.ActionWarn
	case "rewrite":
		d.Action = hooks.ActionRewrite
		d.Rewrite = m["rewrite"]
	}
	d.Message, _ = m["message"].(string)
	return d, nil
}
