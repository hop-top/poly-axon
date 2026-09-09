// Package hosts registers every axon hook codec. Import it for side
// effects; import an individual host package to register only that one.
package hosts

import (
	"hop.top/axon/hooks"
	"hop.top/axon/hooks/hosts/claude"
	"hop.top/axon/hooks/hosts/codex"
	"hop.top/axon/hooks/hosts/gemini"
	"hop.top/axon/hooks/hosts/opencode"
)

func init() {
	hooks.Register(claude.New())
	hooks.Register(gemini.New())
	hooks.Register(codex.New())
	hooks.Register(opencode.New())
}
