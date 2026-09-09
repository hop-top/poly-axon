// Package claude implements the axon hooks.Codec for Claude Code.
package claude

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
	// axon.Event (hooks.Capabilities.DecodeMap). Claude's host names equal
	// the canonical names, so this map is an identity — but going through
	// it is what makes an unrecognized hook_event_name an error instead of
	// an axon.Event carrying whatever string the wire held.
	hostToCanonical map[string]axon.Event
}

// New returns the Claude Code codec. Panics if the spec lacks the host,
// which the conformance test prevents.
func New() hooks.Codec {
	h, ok := axon.Get(axon.HostClaude)
	if !ok {
		panic("axon: claude host missing from spec")
	}
	caps, err := hooks.LoadCapabilities(axon.HostClaude)
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

// EncodeInput builds Claude's native hook stdin envelope. Like the other
// three codecs it requires the event be native or close: EncodeInput
// produces a payload the host itself would have emitted, and a
// synthesized event is derived by the runtime from some other host event,
// so it has no envelope of its own. Claude's capability file is all-native
// today, which makes "classified" and "native or close" coincide — the
// distinction is enforced anyway so adding a synthesized row later cannot
// quietly start emitting envelopes Claude never sends.
func (c *codec) EncodeInput(in hooks.Input) ([]byte, error) {
	if lvl, ok := c.caps.Level(in.Event); !ok || (lvl != hooks.LevelNative && lvl != hooks.LevelClose) {
		return nil, fmt.Errorf("%w: %s/%s", hooks.ErrUnsupportedEvent, c.host.Name, in.Event)
	}
	// Extra carries the host-specific keys the canonical Input does not
	// name, so it is written FIRST and every canonical field below
	// overwrites a colliding key. Written last it clobbered the envelope's
	// own hook_event_name and session_id, emitting a payload naming an
	// event other than in.Event with a nil error.
	out := map[string]any{}
	for k, v := range in.Extra {
		out[k] = v
	}
	out["hook_event_name"] = string(in.Event)
	out["session_id"] = in.SessionID
	out["cwd"] = in.Cwd
	switch in.Event {
	case axon.EventPreToolUse, axon.EventPermissionRequest:
		out["tool_name"], out["tool_input"] = in.ToolName, in.ToolInput
	case axon.EventPostToolUse:
		out["tool_name"], out["tool_input"], out["tool_response"] = in.ToolName, in.ToolInput, in.ToolResponse
	case axon.EventUserPromptSubmit:
		out["prompt"] = in.Prompt
	}
	return json.Marshal(out)
}

// DecodeInput parses Claude's native hook stdin envelope. The event is
// looked up in the capability map rather than trusted verbatim: a missing
// hook_event_name is a malformed payload (ErrSchema) and an unrecognized
// one is an event Claude does not speak (ErrUnsupportedEvent). Accepting
// either used to hand the caller an Input whose Event was "" or an
// arbitrary string, with a nil error.
func (c *codec) DecodeInput(raw []byte) (hooks.Input, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return hooks.Input{}, fmt.Errorf("%w: claude input: %v", hooks.ErrSchema, err)
	}
	hostEvent, ok := m["hook_event_name"].(string)
	if !ok || hostEvent == "" {
		return hooks.Input{}, fmt.Errorf("%w: claude input: missing hook_event_name", hooks.ErrSchema)
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

// nativeAction maps canonical actions to Claude's PreToolUse
// permissionDecision. warn has no distinct exit code in Claude's hook
// contract; it still surfaces as the native "ask" permissionDecision on
// the PreToolUse hookSpecificOutput channel.
var nativeAction = map[hooks.Action]string{
	hooks.ActionAllow: "allow", hooks.ActionWarn: "ask", hooks.ActionBlock: "deny",
}

// EncodeDecision picks the wire shape by d.Event: a blank Event or
// PreToolUse use the tool-gate hookSpecificOutput.permissionDecision
// shape; PermissionRequest, PostToolUse and SessionStart each have their
// own native shape (see the per-event encode* helpers). Any other event
// has no decision channel in Claude's hook contract.
func (c *codec) EncodeDecision(d hooks.Decision) ([]byte, int, error) {
	// Capability gate first, so an event the capability file marks
	// unsupported is rejected for that reason rather than incidentally by
	// the shape switch below. A blank Event keeps the default shape.
	if d.Event != "" {
		if lvl, ok := c.caps.Level(d.Event); !ok || (lvl != hooks.LevelNative && lvl != hooks.LevelClose) {
			return nil, 0, fmt.Errorf("%w: %s/%s", hooks.ErrUnsupportedEvent, c.host.Name, d.Event)
		}
	}
	code := c.exitFor(d.Action)
	switch d.Event {
	case "", axon.EventPreToolUse:
		b, err := encodePreToolUseDecision(d)
		return b, code, err
	case axon.EventPermissionRequest:
		b, err := encodePermissionRequestDecision(d)
		return b, code, err
	case axon.EventPostToolUse:
		b, err := encodePostToolUseDecision(d)
		return b, code, err
	case axon.EventSessionStart:
		b, err := encodeSessionStartDecision(d)
		return b, code, err
	default:
		return nil, 0, fmt.Errorf("%w: claude decision for %s", hooks.ErrUnsupportedEvent, d.Event)
	}
}

// encodePreToolUseDecision is Claude's default tool-gate shape:
// hookSpecificOutput.permissionDecision (allow/ask/deny) plus
// updatedInput for rewrite. This is the shape a blank Decision.Event
// falls back to.
func encodePreToolUseDecision(d hooks.Decision) ([]byte, error) {
	switch d.Action {
	case hooks.ActionAllow:
		return []byte(`{}`), nil
	case hooks.ActionRewrite:
		out := map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": "allow", "updatedInput": d.Rewrite}}
		return json.Marshal(out)
	case hooks.ActionBlock, hooks.ActionWarn:
		// Two-value lookup: an action with no native permissionDecision
		// used to marshal permissionDecision:"" with a nil error, so a
		// malformed envelope reached the wire and the host read it as an
		// unset decision.
		native, ok := nativeAction[d.Action]
		if !ok {
			return nil, fmt.Errorf("%w: claude cannot express action %q", hooks.ErrUnsupportedAction, d.Action)
		}
		out := map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": native,
			"permissionDecisionReason": d.Message}}
		return json.Marshal(out)
	default:
		return nil, fmt.Errorf("%w: claude cannot express action %q", hooks.ErrUnsupportedAction, d.Action)
	}
}

// encodePermissionRequestDecision mirrors nerv's adapters/claude
// translate.go buildBlockOutput/buildRewriteOutput PermissionRequest
// branches: hookSpecificOutput.decision.{behavior,message|updatedInput}.
// warn has no native channel on this event (nerv only defines
// allow/block/rewrite for PermissionRequest) so it degrades to allow,
// matching nerv's own warn-degrades-to-allow rule.
func encodePermissionRequestDecision(d hooks.Decision) ([]byte, error) {
	switch d.Action {
	case hooks.ActionAllow, hooks.ActionWarn:
		return []byte(`{}`), nil
	case hooks.ActionBlock:
		out := map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PermissionRequest",
			"decision": map[string]any{
				"behavior": "deny",
				"message":  d.Message,
			},
		}}
		return json.Marshal(out)
	case hooks.ActionRewrite:
		out := map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PermissionRequest",
			"decision": map[string]any{
				"behavior":     "allow",
				"updatedInput": d.Rewrite,
			},
		}}
		return json.Marshal(out)
	default:
		return nil, fmt.Errorf("%w: claude decision %s for %s", hooks.ErrUnsupportedAction, d.Action, axon.EventPermissionRequest)
	}
}

// encodePostToolUseDecision mirrors nerv's buildBlockOutput default
// branch (used for every event but PreToolUse/PermissionRequest):
// top-level decision/reason. PostToolUse has no rewrite channel in
// Claude's hook contract.
func encodePostToolUseDecision(d hooks.Decision) ([]byte, error) {
	switch d.Action {
	case hooks.ActionAllow, hooks.ActionWarn:
		return []byte(`{}`), nil
	case hooks.ActionBlock:
		out := map[string]any{"decision": "block", "reason": d.Message}
		return json.Marshal(out)
	default:
		return nil, fmt.Errorf("%w: claude decision %s for %s", hooks.ErrUnsupportedAction, d.Action, axon.EventPostToolUse)
	}
}

// encodeSessionStartDecision: a contextless SessionStart allow is the
// empty envelope. An allow carrying context in d.Metadata (the three
// keys encodeSessionStartContext recognizes) emits Claude's native
// hookSpecificOutput.additionalContext / systemMessage / continue shape
// instead — context injection is the entire point of a SessionStart
// hook, so it must round-trip through Metadata rather than being
// dropped. nerv's buildBlockOutput has no dedicated SessionStart case,
// so a SessionStart block falls to nerv's generic default branch — the
// same top-level decision/reason shape as PostToolUse. SessionStart has
// no rewrite channel in Claude's hook contract.
func encodeSessionStartDecision(d hooks.Decision) ([]byte, error) {
	switch d.Action {
	case hooks.ActionAllow, hooks.ActionWarn:
		if b := encodeSessionStartContext(d.Metadata); b != nil {
			return b, nil
		}
		return []byte(`{}`), nil
	case hooks.ActionBlock:
		out := map[string]any{"decision": "block", "reason": d.Message}
		return json.Marshal(out)
	default:
		return nil, fmt.Errorf("%w: claude decision %s for %s", hooks.ErrUnsupportedAction, d.Action, axon.EventSessionStart)
	}
}

// encodeSessionStartContext builds Claude's SessionStart context-injection
// shape from Decision.Metadata's three recognized keys
// (additional_context: string, system_message: string, continue: bool),
// or returns nil when none are set so the caller falls back to the bare
// `{}` envelope. A key of the wrong type, or absent, is silently skipped
// rather than erroring — Metadata is a best-effort side channel, not a
// validated input.
func encodeSessionStartContext(metadata map[string]any) []byte {
	if len(metadata) == 0 {
		return nil
	}
	out := map[string]any{}
	if v, ok := metadata["additional_context"].(string); ok && v != "" {
		out["hookSpecificOutput"] = map[string]any{"additionalContext": v}
	}
	if v, ok := metadata["system_message"].(string); ok && v != "" {
		out["systemMessage"] = v
	}
	if v, ok := metadata["continue"].(bool); ok {
		out["continue"] = v
	}
	if len(out) == 0 {
		return nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return b
}

// DecodeDecision classifies stdout by its recognized decision shape.
// Empty stdout, or JSON that carries none of Claude's known decision
// keys, both fall back to actionForExit(exit): the exit code is the
// only remaining signal.
func (c *codec) DecodeDecision(stdout []byte, exit int) (hooks.Decision, error) {
	if len(bytes.TrimSpace(stdout)) == 0 {
		return hooks.Decision{Action: c.actionForExit(exit)}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(stdout, &m); err != nil {
		return hooks.Decision{}, fmt.Errorf("%w: claude decision: %v", hooks.ErrSchema, err)
	}
	d, recognized := decodeKnownShape(m)
	if !recognized {
		return hooks.Decision{Action: c.actionForExit(exit)}, nil
	}
	return d, nil
}

// decodeKnownShape probes the wire map for Claude's known decision
// shapes and reports whether any were found. Event is set whenever the
// wire reveals it: hookSpecificOutput.hookEventName if present;
// hookSpecificOutput.decision.behavior implies PermissionRequest;
// permissionDecision implies PreToolUse. Left blank when nothing
// reveals it (the top-level decision/reason shape is shared by
// PostToolUse and SessionStart, so it cannot disambiguate the event).
func decodeKnownShape(m map[string]any) (hooks.Decision, bool) {
	d := hooks.Decision{Action: hooks.ActionAllow}
	recognized := false

	if hso, ok := m["hookSpecificOutput"].(map[string]any); ok {
		if hen, ok := hso["hookEventName"].(string); ok && hen != "" {
			d.Event = axon.Event(hen)
		}
		if pd, ok := hso["permissionDecision"]; ok {
			recognized = true
			if d.Event == "" {
				d.Event = axon.EventPreToolUse
			}
			switch pd {
			case "deny":
				d.Action = hooks.ActionBlock
			case "ask":
				d.Action = hooks.ActionWarn
			}
			d.Message, _ = hso["permissionDecisionReason"].(string)
			if u, ok := hso["updatedInput"]; ok {
				d.Action, d.Rewrite = hooks.ActionRewrite, u
			}
		}
		if dec, ok := hso["decision"].(map[string]any); ok {
			if behavior, ok := dec["behavior"].(string); ok {
				recognized = true
				d.Event = axon.EventPermissionRequest
				switch behavior {
				case "deny":
					d.Action = hooks.ActionBlock
					d.Message, _ = dec["message"].(string)
				case "allow":
					if u, ok := dec["updatedInput"]; ok {
						d.Action, d.Rewrite = hooks.ActionRewrite, u
					} else {
						d.Action = hooks.ActionAllow
					}
				}
			}
		}
		if ac, ok := hso["additionalContext"].(string); ok && ac != "" {
			recognized = true
			d.Event = axon.EventSessionStart
			d.Action = hooks.ActionAllow
			setMetadata(&d, "additional_context", ac)
		}
	}
	if sm, ok := m["systemMessage"].(string); ok && sm != "" {
		recognized = true
		d.Event = axon.EventSessionStart
		setMetadata(&d, "system_message", sm)
	}
	if cont, ok := m["continue"].(bool); ok {
		recognized = true
		d.Event = axon.EventSessionStart
		setMetadata(&d, "continue", cont)
	}
	if dec, ok := m["decision"].(string); ok && dec == "block" {
		recognized = true
		d.Action = hooks.ActionBlock
		d.Message, _ = m["reason"].(string)
	}
	return d, recognized
}

// setMetadata lazily allocates d.Metadata and sets key. Decision.Metadata
// is nil by default (no codec allocates it for a contextless decision),
// so every writer must guard the nil map the same way.
func setMetadata(d *hooks.Decision, key string, value any) {
	if d.Metadata == nil {
		d.Metadata = map[string]any{}
	}
	d.Metadata[key] = value
}

// exitFor maps a canonical action to Claude's process exit code, sourced
// from spec/hosts/claude/host.yaml (axon.Host.ExitCodes). warn has no
// distinct exit code in Claude's contract (host.yaml sets no "warn" key,
// leaving ExitCodes.Warn at its zero value); it exits like allow.
func (c *codec) exitFor(a hooks.Action) int {
	switch a {
	case hooks.ActionBlock:
		return c.host.ExitCodes.Block
	case hooks.ActionRewrite:
		return c.host.ExitCodes.Rewrite
	default:
		return c.host.ExitCodes.Allow
	}
}

// actionForExit is the inverse of exitFor, used to classify decisions
// with no recognized JSON decision shape (including empty stdout) by
// exit code alone.
func (c *codec) actionForExit(exit int) hooks.Action {
	switch exit {
	case c.host.ExitCodes.Block:
		return hooks.ActionBlock
	case c.host.ExitCodes.Rewrite:
		return hooks.ActionRewrite
	default:
		return hooks.ActionAllow
	}
}
