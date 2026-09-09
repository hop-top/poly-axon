package hooks

import "hop.top/axon"

// Decision is the canonical, host-independent shape of a hook handler's
// result. A Codec encodes it into a host's native stdout/exit-code
// convention and decodes that convention back into a Decision.
type Decision struct {
	Action  Action
	Message string
	Rewrite any
	// Event names the hook event this decision answers. Codecs whose wire
	// shape varies by event use it on encode; a blank Event means the
	// host's default tool-gate shape (PreToolUse). Decoders set it when the
	// wire reveals the event, otherwise leave it blank.
	Event    axon.Event
	Metadata map[string]any
}
