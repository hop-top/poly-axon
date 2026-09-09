// Package codex implements the hooks.Codec for the Codex CLI. Codex's
// handler contract mirrors Claude Code's: stdin JSON envelope, stdout
// JSON decision, exit code (nerv docs/research/codex-cli.md). Codec
// decisions are therefore Claude-shape JSON, with the exit code used as
// a fallback only when stdout is empty.
package codex

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
	// axon.Event (hooks.Capabilities.DecodeMap). Codex's host names equal
	// the canonical names, so this map is an identity — but going through
	// it is what makes an unrecognized hook_event_name an error instead of
	// an axon.Event carrying whatever string the wire held.
	hostToCanonical map[string]axon.Event
}

// New returns the Codex CLI codec. Panics if the spec lacks the host,
// which the conformance test prevents.
func New() hooks.Codec {
	h, ok := axon.Get(axon.HostCodex)
	if !ok {
		panic("axon: codex host missing from spec")
	}
	caps, err := hooks.LoadCapabilities(axon.HostCodex)
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

func (c *codec) EncodeInput(in hooks.Input) ([]byte, error) {
	if lvl, ok := c.caps.Level(in.Event); !ok || (lvl != hooks.LevelNative && lvl != hooks.LevelClose) {
		return nil, fmt.Errorf("%w: %s/%s", hooks.ErrUnsupportedEvent, c.host.Name, in.Event)
	}
	// Extra is written FIRST; the canonical fields below overwrite any
	// colliding key. Written last it clobbered hook_event_name and
	// session_id — see the claude codec for the same note.
	out := map[string]any{}
	for k, v := range in.Extra {
		out[k] = v
	}
	out["hook_event_name"] = string(in.Event)
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

// DecodeInput parses Codex's native hook stdin envelope. The event is
// looked up in the capability map rather than trusted verbatim: a missing
// hook_event_name is a malformed payload (ErrSchema) and an unrecognized
// one is an event Codex does not speak (ErrUnsupportedEvent). Accepting
// either used to hand the caller an Input whose Event was "" or an
// arbitrary string, with a nil error.
func (c *codec) DecodeInput(raw []byte) (hooks.Input, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return hooks.Input{}, fmt.Errorf("%w: codex input: %v", hooks.ErrSchema, err)
	}
	hostEvent, ok := m["hook_event_name"].(string)
	if !ok || hostEvent == "" {
		return hooks.Input{}, fmt.Errorf("%w: codex input: missing hook_event_name", hooks.ErrSchema)
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

// nativeAction maps canonical actions to Claude-shape permissionDecision,
// same mapping Task 7 uses for claude: deny is canonical block, ask is
// canonical warn.
var nativeAction = map[hooks.Action]string{
	hooks.ActionAllow: "allow", hooks.ActionWarn: "ask", hooks.ActionBlock: "deny",
}

// EncodeDecision picks the wire shape by d.Event, mirroring the claude
// codec: a blank Event or PreToolUse use the tool-gate
// hookSpecificOutput.permissionDecision shape; PostToolUse uses the
// top-level decision/reason shape (nerv's claude translate.go
// buildBlockOutput default branch); SessionStart allows with the empty
// envelope and blocks with the same top-level shape (claude has no
// dedicated SessionStart block shape either). UserPromptSubmit and Stop
// allow with the empty envelope; nerv's claude translate.go defines no
// dedicated block shape for either, so — same as the claude codec itself —
// block on those events is unsupported. Any other event is unsupported.
func (c *codec) EncodeDecision(d hooks.Decision) ([]byte, int, error) {
	// Capability gate first, mirroring the other three codecs: an event
	// the capability file marks unsupported is rejected for that reason
	// rather than incidentally by the shape switch. A blank Event keeps
	// the default shape.
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
	case axon.EventPostToolUse:
		b, err := encodePostToolUseDecision(d)
		return b, code, err
	case axon.EventSessionStart:
		b, err := encodeSessionStartDecision(d)
		return b, code, err
	case axon.EventUserPromptSubmit, axon.EventStop:
		switch d.Action {
		case hooks.ActionAllow, hooks.ActionWarn:
			return []byte(`{}`), code, nil
		default:
			return nil, 0, fmt.Errorf("%w: codex decision %s for %s", hooks.ErrUnsupportedAction, d.Action, d.Event)
		}
	default:
		return nil, 0, fmt.Errorf("%w: codex decision for %s", hooks.ErrUnsupportedEvent, d.Event)
	}
}

// encodePreToolUseDecision is Codex's default tool-gate shape, identical
// to Claude's: hookSpecificOutput.permissionDecision (allow/ask/deny)
// plus updatedInput for rewrite. This is the shape a blank Decision.Event
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
		// Two-value lookup, same as claude: an action with no native
		// permissionDecision used to marshal permissionDecision:"" with a
		// nil error, putting a malformed envelope on the wire.
		native, ok := nativeAction[d.Action]
		if !ok {
			return nil, fmt.Errorf("%w: codex cannot express action %q", hooks.ErrUnsupportedAction, d.Action)
		}
		out := map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": native,
			"permissionDecisionReason": d.Message}}
		return json.Marshal(out)
	default:
		return nil, fmt.Errorf("%w: codex cannot express action %q", hooks.ErrUnsupportedAction, d.Action)
	}
}

// encodePostToolUseDecision mirrors nerv's buildBlockOutput default
// branch: top-level decision/reason. PostToolUse has no rewrite channel.
func encodePostToolUseDecision(d hooks.Decision) ([]byte, error) {
	switch d.Action {
	case hooks.ActionAllow, hooks.ActionWarn:
		return []byte(`{}`), nil
	case hooks.ActionBlock:
		out := map[string]any{"decision": "block", "reason": d.Message}
		return json.Marshal(out)
	default:
		return nil, fmt.Errorf("%w: codex decision %s for %s", hooks.ErrUnsupportedAction, d.Action, axon.EventPostToolUse)
	}
}

// encodeSessionStartDecision: SessionStart allow is the empty envelope;
// block falls to nerv's generic default branch, the same top-level
// decision/reason shape as PostToolUse. No rewrite channel.
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
		return nil, fmt.Errorf("%w: codex decision %s for %s", hooks.ErrUnsupportedAction, d.Action, axon.EventSessionStart)
	}
}

// encodeSessionStartContext builds codex's SessionStart context-injection
// shape from Decision.Metadata's three recognized keys
// (additional_context: string, system_message: string, continue: bool),
// identical to claude's shape, or returns nil when none are set so the
// caller falls back to the bare `{}` envelope. A key of the wrong type,
// or absent, is silently skipped rather than erroring — Metadata is a
// best-effort side channel, not a validated input.
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

// DecodeDecision classifies stdout by its recognized decision shape,
// mirroring the claude codec's decodeKnownShape. Empty stdout, or JSON
// that carries none of the known decision keys, both fall back to
// actionForExit(exit).
func (c *codec) DecodeDecision(stdout []byte, exit int) (hooks.Decision, error) {
	if len(bytes.TrimSpace(stdout)) == 0 {
		return hooks.Decision{Action: c.actionForExit(exit)}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(stdout, &m); err != nil {
		return hooks.Decision{}, fmt.Errorf("%w: codex decision: %v", hooks.ErrSchema, err)
	}
	d, recognized := decodeKnownShape(m)
	if !recognized {
		return hooks.Decision{Action: c.actionForExit(exit)}, nil
	}
	return d, nil
}

// decodeKnownShape probes the wire map for codex's known decision shapes
// (identical to claude's PreToolUse/PostToolUse/SessionStart shapes; codex
// has no PermissionRequest channel) and reports whether any were found.
// Event is set whenever the wire reveals it: hookSpecificOutput.hookEventName
// if present; permissionDecision implies PreToolUse. Left blank when
// nothing reveals it (the top-level decision/reason shape is shared by
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

// exitFor returns the host's exit code for a canonical action, from
// spec/hosts/codex/host.yaml (the same exit-code contract as claude).
func (c *codec) exitFor(a hooks.Action) int {
	switch a {
	case hooks.ActionAllow:
		return c.host.ExitCodes.Allow
	case hooks.ActionWarn:
		return c.host.ExitCodes.Warn
	case hooks.ActionBlock:
		return c.host.ExitCodes.Block
	case hooks.ActionRewrite:
		return c.host.ExitCodes.Rewrite
	default:
		return c.host.ExitCodes.Allow
	}
}

// actionForExit is the inverse of exitFor, used when stdout is empty and
// the exit code is the only signal available. Codex's host.yaml (like
// claude's) leaves warn unset, defaulting it to 0 same as allow, so an
// unset warn code can never win a match here: allow's exit (also 0)
// takes it as the default case.
func (c *codec) actionForExit(exit int) hooks.Action {
	switch {
	case exit == c.host.ExitCodes.Block:
		return hooks.ActionBlock
	case exit == c.host.ExitCodes.Rewrite:
		return hooks.ActionRewrite
	case c.host.ExitCodes.Warn != 0 && exit == c.host.ExitCodes.Warn:
		return hooks.ActionWarn
	default:
		return hooks.ActionAllow
	}
}
