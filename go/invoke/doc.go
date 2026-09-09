// Package invoke builds native argv for agent CLIs (Claude, Codex,
// Gemini, OpenCode, …) from one normalized Invocation. Adapters
// live at hop.top/axon/invoke/adapters/<cli>/.
//
// Build is pure: it returns a CommandSpec plus a Diagnostics slice
// describing every shim or unsupported option encountered. Execution
// (Runner) is optional and side-effecting; callers wire it explicitly.
//
// See invoke/README.md for the adapter catalog and option mappings.
package invoke
