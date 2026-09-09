// Package gemini implements the axon hooks Codec for Gemini CLI.
package gemini

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
	// hostToCanonical maps a host-side event name to its canonical
	// axon.Event. Built once in New by hooks.Capabilities.DecodeMap, where
	// native rows win over close rows: gemini lists host event SessionEnd
	// native -> SessionEnd and close -> Stop, and only the native row says
	// what Gemini actually emitted.
	hostToCanonical map[string]axon.Event
}

// New returns the Gemini CLI codec. Panics if the spec lacks the host,
// which the conformance test prevents.
func New() hooks.Codec {
	h, ok := axon.Get(axon.HostGemini)
	if !ok {
		panic("axon: gemini host missing from spec")
	}
	caps, err := hooks.LoadCapabilities(axon.HostGemini)
	if err != nil {
		panic(err)
	}
	rev, err := caps.DecodeMap()
	if err != nil {
		panic(err)
	}
	return &codec{host: h, caps: caps, hostToCanonical: rev}
}

func (c *codec) Host() string                     { return c.host.Name }
func (c *codec) Capabilities() hooks.Capabilities { return c.caps }

// EncodeInput builds Gemini's native hook stdin envelope. It fails with
// ErrUnsupportedEvent when the event has no native or close mapping
// (synthesized, unsupported, or unclassified events cannot be encoded as
// a Gemini-native payload since Gemini itself never emits them).
func (c *codec) EncodeInput(in hooks.Input) ([]byte, error) {
	level, ok := c.caps.Level(in.Event)
	if !ok || (level != hooks.LevelNative && level != hooks.LevelClose) {
		return nil, fmt.Errorf("%w: %s/%s", hooks.ErrUnsupportedEvent, c.host.Name, in.Event)
	}
	hostEvent, ok := c.caps.HostEvent(in.Event)
	if !ok {
		return nil, fmt.Errorf("%w: %s/%s", hooks.ErrUnsupportedEvent, c.host.Name, in.Event)
	}
	// Extra is written FIRST; the canonical fields below overwrite any
	// colliding key. Written last it clobbered hook_event_name and
	// session_id — see the claude codec for the same note.
	out := map[string]any{}
	for k, v := range in.Extra {
		out[k] = v
	}
	out["hook_event_name"] = hostEvent
	out["session_id"] = in.SessionID
	out["cwd"] = in.Cwd
	switch in.Event {
	case axon.EventPreToolUse:
		out["tool_name"], out["tool_input"] = in.ToolName, in.ToolInput
	case axon.EventPostToolUse:
		out["tool_name"], out["tool_input"], out["tool_response"] = in.ToolName, in.ToolInput, in.ToolResponse
	case axon.EventUserPromptSubmit:
		out["prompt"] = in.Prompt
	}
	return json.Marshal(out)
}

// DecodeInput parses Gemini's native hook stdin envelope. The canonical
// event is looked up in the native-wins decode map on hook_event_name; a
// name that is not there is ErrSchema, never a silent fallback.
func (c *codec) DecodeInput(raw []byte) (hooks.Input, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return hooks.Input{}, fmt.Errorf("%w: gemini input: %v", hooks.ErrSchema, err)
	}
	hostEvent, ok := m["hook_event_name"].(string)
	if !ok || hostEvent == "" {
		return hooks.Input{}, fmt.Errorf("%w: gemini input: missing hook_event_name", hooks.ErrSchema)
	}
	ev, known := c.hostToCanonical[hostEvent]
	if !known {
		return hooks.Input{}, fmt.Errorf("%w: %s/%s", hooks.ErrUnsupportedEvent, c.host.Name, hostEvent)
	}
	in := hooks.Input{Event: ev, Extra: map[string]any{}}
	in.SessionID, _ = m["session_id"].(string)
	in.Cwd, _ = m["cwd"].(string)
	in.ToolName, _ = m["tool_name"].(string)
	in.ToolInput, _ = m["tool_input"].(map[string]any)
	in.ToolResponse, _ = m["tool_response"].(map[string]any)
	in.Prompt, _ = m["prompt"].(string)
	for k, v := range m {
		switch k {
		case "hook_event_name", "session_id", "cwd", "tool_name", "tool_input", "tool_response", "prompt":
		default:
			in.Extra[k] = v
		}
	}
	return in, nil
}

// EncodeDecision emits Gemini's native decision envelope. Gemini has no
// native "warn" decision; nerv's adapters/gemini/translate.go
// (FromDecision) folds warn into decision "allow" and, when the message
// is non-empty, carries it in BOTH "reason" and "systemMessage" (unlike
// allow/block, which only ever set "reason"). This codec mirrors that
// fold exactly rather than erroring: warn is a real, defined outcome in
// nerv's wire contract, so ErrUnsupportedEvent is reserved for actions
// nerv itself cannot express at all.
func (c *codec) EncodeDecision(d hooks.Decision) ([]byte, int, error) {
	if err := c.checkEvent(d.Event); err != nil {
		return nil, 0, err
	}
	switch d.Action {
	case hooks.ActionAllow:
		b, err := json.Marshal(map[string]any{"decision": "allow"})
		return b, c.host.ExitCodes.Allow, err
	case hooks.ActionWarn:
		// Mirrors FromDecision's Warn branch: decision stays "allow";
		// a non-empty message is duplicated into both "reason" and
		// "systemMessage" (the second field is what makes this
		// decodable back to warn instead of plain allow).
		out := map[string]any{"decision": "allow"}
		if d.Message != "" {
			out["reason"] = d.Message
			out["systemMessage"] = d.Message
		}
		b, err := json.Marshal(out)
		// host.yaml carries no warn exit code for gemini; FromDecision
		// only raises the exit code for Block, so warn exits like allow.
		return b, c.host.ExitCodes.Allow, err
	case hooks.ActionRewrite:
		out := map[string]any{
			"decision": "allow",
			"hookSpecificOutput": map[string]any{
				"tool_input": d.Rewrite,
			},
		}
		b, err := json.Marshal(out)
		// host.yaml carries no rewrite exit code for gemini; rewrite still
		// allows the tool call to proceed, so it exits like allow.
		return b, c.host.ExitCodes.Allow, err
	case hooks.ActionBlock:
		b, err := json.Marshal(map[string]any{"decision": "deny", "reason": d.Message})
		return b, c.host.ExitCodes.Block, err
	default:
		// Exhaustive over the four canonical actions; anything else —
		// including the zero Action — is a caller bug, not a wire shape.
		return nil, 0, fmt.Errorf("%w: gemini cannot express action %q", hooks.ErrUnsupportedAction, d.Action)
	}
}

// checkEvent rejects a decision naming an event Gemini has no wire shape
// for. A blank Event keeps the default shape, per hooks.Decision.Event's
// contract; anything the capability file does not mark native or close
// (TeammateIdle, StopFailure, an unclassified name) is
// ErrUnsupportedEvent rather than a decision emitted for an event Gemini
// never raises.
func (c *codec) checkEvent(ev axon.Event) error {
	if ev == "" {
		return nil
	}
	lvl, ok := c.caps.Level(ev)
	if !ok || (lvl != hooks.LevelNative && lvl != hooks.LevelClose) {
		return fmt.Errorf("%w: %s/%s", hooks.ErrUnsupportedEvent, c.host.Name, ev)
	}
	return nil
}

// DecodeDecision reads Gemini's native decision envelope. Empty stdout,
// or JSON that carries none of Gemini's known decision keys, both fall
// back to actionForExit(exit) per Gemini's exit_codes contract (allow=0,
// block=2): the exit code is the only remaining signal. Without this
// gate, unrecognized JSON (including a bare `{}`) silently decoded to
// allow regardless of exit code — this is what decodeKnownShape's
// "recognized" return value now prevents, mirroring the claude codec.
func (c *codec) DecodeDecision(stdout []byte, exit int) (hooks.Decision, error) {
	if len(bytes.TrimSpace(stdout)) == 0 {
		return hooks.Decision{Action: c.actionForExit(exit)}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(stdout, &m); err != nil {
		return hooks.Decision{}, fmt.Errorf("%w: gemini decision: %v", hooks.ErrSchema, err)
	}
	d, recognized := decodeKnownShape(m)
	if !recognized {
		return hooks.Decision{Action: c.actionForExit(exit)}, nil
	}
	return d, nil
}

// decodeKnownShape probes the wire map for Gemini's known decision
// shapes and reports whether any were found. The systemMessage-based
// warn/allow fold is preserved exactly as before this gate was added:
// it is about disambiguating two shapes DecodeDecision already
// recognizes (both carry "decision"), not about recognizing a shape in
// the first place — so "decision" is what sets recognized, and
// systemMessage/hookSpecificOutput.tool_input only refine an
// already-recognized decision.
func decodeKnownShape(m map[string]any) (hooks.Decision, bool) {
	d := hooks.Decision{Action: hooks.ActionAllow}
	recognized := false

	switch m["decision"] {
	case "deny":
		recognized = true
		d.Action = hooks.ActionBlock
	case "allow":
		recognized = true
		d.Action = hooks.ActionAllow
	}
	d.Message, _ = m["reason"].(string)
	// FromDecision's warn fold is lossy on the wire: a plain allow and a
	// warn both carry decision:"allow" plus "reason". The only signal
	// that distinguishes them is "systemMessage", which FromDecision sets
	// exclusively for the Warn branch. Its absence means this genuinely
	// cannot be told apart from allow, so it decodes to allow (documented
	// here rather than guessed at by the caller).
	if sysMsg, ok := m["systemMessage"].(string); ok && sysMsg != "" {
		d.Action = hooks.ActionWarn
	}
	if hso, ok := m["hookSpecificOutput"].(map[string]any); ok {
		if u, ok := hso["tool_input"]; ok {
			recognized = true
			d.Action, d.Rewrite = hooks.ActionRewrite, u
		}
	}
	return d, recognized
}

// actionForExit inverts host.ExitCodes for the empty-stdout case.
func (c *codec) actionForExit(exit int) hooks.Action {
	switch exit {
	case c.host.ExitCodes.Block:
		if exit != c.host.ExitCodes.Allow {
			return hooks.ActionBlock
		}
	}
	return hooks.ActionAllow
}
