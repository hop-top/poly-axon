// Package axon is the host-CLI contract for AI-assistant hooks: host
// identity (names, aliases, binaries, config paths, store roots, exit-code
// conventions), a canonical event catalog, and per-host capability maps
// for Claude Code, Gemini CLI, Codex, OpenCode and thirteen more.
//
// The embedded spec/ tree is the contract, not this code: every lookup
// here reads that YAML and JSON Schema data, so Go, TypeScript and Python
// answer from one source. This module is the reference implementation the
// other two are checked against.
//
// Hook payload codecs live in hop.top/axon/hooks; native argv building
// lives in hop.top/axon/invoke.
package axon
