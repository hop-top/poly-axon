// Package all lists every built-in invocation adapter. It exists to
// break the import cycle that a package-level Adapters() inside
// invoke itself would create (adapters import invoke).
package all

import (
	"sort"

	"hop.top/axon/invoke"
	"hop.top/axon/invoke/adapters/claude"
	"hop.top/axon/invoke/adapters/codex"
	"hop.top/axon/invoke/adapters/copilot"
	"hop.top/axon/invoke/adapters/crush"
	"hop.top/axon/invoke/adapters/cursoragent"
	"hop.top/axon/invoke/adapters/gemini"
	"hop.top/axon/invoke/adapters/goose"
	"hop.top/axon/invoke/adapters/kimi"
	"hop.top/axon/invoke/adapters/opencode"
	"hop.top/axon/invoke/adapters/qwen"
	"hop.top/axon/invoke/adapters/vibe"
)

// Adapters returns every built-in adapter, sorted by CLI() name.
func Adapters() []invoke.InvocationAdapter {
	list := []invoke.InvocationAdapter{
		claude.New(), codex.New(), copilot.New(), crush.New(),
		cursoragent.New(), gemini.New(), goose.New(), kimi.New(),
		opencode.New(), qwen.New(), vibe.New(),
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CLI() < list[j].CLI() })
	return list
}
