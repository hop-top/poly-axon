package hooks

import "hop.top/axon"

// Action is the canonical outcome a hook handler returns for an event.
type Action string

const (
	ActionAllow   Action = "allow"
	ActionWarn    Action = "warn"
	ActionBlock   Action = "block"
	ActionRewrite Action = "rewrite"
)

// Input is the canonical, host-independent shape of a hook invocation's
// input. A Codec translates a host's native payload into an Input and
// back; fields the canonical set does not name land in Extra.
type Input struct {
	Event        axon.Event
	SessionID    string
	Cwd          string
	ToolName     string
	ToolInput    map[string]any
	ToolResponse map[string]any
	Prompt       string
	Extra        map[string]any
}
