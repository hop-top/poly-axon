package capture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"hop.top/axon"
)

// Snippet is the configuration an operator adds to one host, by hand, to
// route its hook events at the capture handler.
//
// The tool prints this; it never writes it. A host's settings file is the
// operator's, it carries their unrelated configuration, and installing a
// hook is a decision — so the snippet is output, and applying it is a
// deliberate edit made by the person whose machine it is.
type Snippet struct {
	Host string
	// ConfigPaths are the candidate settings files, in the order
	// spec/hosts/<host>/host.yaml's hook_config_paths lists them: the
	// user-level file first where the host puts it there, the
	// project-level file otherwise. The list is copied from the spec, not
	// remembered here.
	ConfigPaths []string
	// HostEvents are the host-side event names the snippet subscribes to,
	// read off the host's capabilities.yaml native and close rows.
	HostEvents []string
	// Body is the text to add to the settings file.
	Body string
	// Language names Body's syntax, for a fenced code block.
	Language string
	// Warning is a host-specific caveat an operator must read before
	// installing, or empty.
	Warning string
}

// SnippetFor builds the install snippet for one host. argv is the handler
// command as a real argument vector — binary first — which the caller
// composes (the CLI passes its own resolved binary path and capture
// directory).
//
// It is an argv and not a command line on purpose. Joining the parts into
// a string and re-splitting them on spaces destroyed any path containing
// one, and "~/Library/Application Support/..." is an ordinary place for a
// binary or a capture directory to live. The opencode plugin needs the
// parts separately anyway (node's spawn takes command and args), and the
// three settings-file hosts get a properly quoted line.
//
// Only the pairs still missing a fixture are subscribed by default, so the
// handler sees the events that need capturing and stays out of the way of
// the ones already recorded. Pass all=true to subscribe every event the
// host raises.
func SnippetFor(host string, argv []string, all bool) (Snippet, error) {
	h, ok := axon.Get(host)
	if !ok {
		return Snippet{}, fmt.Errorf("capture: unknown host %q", host)
	}
	if !h.Hooks {
		return Snippet{}, fmt.Errorf("capture: %s has no hook surface (spec/hosts/%s/host.yaml sets hooks: false)", host, host)
	}
	pairs, err := Coverage("")
	if err != nil {
		return Snippet{}, err
	}
	seen := map[string]bool{}
	var hostEvents []string
	for _, p := range pairs {
		if p.Host != host {
			continue
		}
		if !all && p.Status == StatusFixture {
			continue
		}
		if !seen[p.HostEvent] {
			seen[p.HostEvent] = true
			hostEvents = append(hostEvents, p.HostEvent)
		}
	}
	sort.Strings(hostEvents)
	s := Snippet{Host: host, ConfigPaths: h.HookConfigPaths, HostEvents: hostEvents}
	switch host {
	case axon.HostClaude, axon.HostGemini:
		// Both read a settings.json whose "hooks" object is keyed by the
		// host-side event name, each holding matcher groups of hook
		// commands. hook_config_paths in each host.yaml names the two
		// settings.json files (user-level, then project-level).
		s.Body, err = settingsJSONHooks(hostEvents, shellJoin(argv))
		s.Language = "json"
	case axon.HostCodex:
		// codex's hook_config_paths names hooks.json rather than a
		// settings.json, so the object is the file's whole content rather
		// than a key inside a larger settings document.
		s.Body, err = codexHooksJSON(hostEvents, shellJoin(argv))
		s.Language = "json"
	case axon.HostOpencode:
		// OpenCode hooks are in-process plugin callbacks, not
		// subprocesses: host.yaml omits exit_codes entirely and its
		// hook_config_paths names a TypeScript plugin file. The snippet is
		// therefore a plugin module that shells out to the handler, not a
		// settings entry.
		s.Body = opencodePlugin(hostEvents, argv)
		s.Language = "typescript"
		s.Warning = "OpenCode hooks run in-process inside the CLI, so this plugin shells out to the handler and ignores its result. A throw inside a plugin callback is how an OpenCode hook blocks (spec/hosts/opencode/host.yaml's exit_codes note), so the callback body must never throw."
	default:
		return Snippet{}, fmt.Errorf("capture: no install snippet for host %q", host)
	}
	if err != nil {
		return Snippet{}, err
	}
	return s, nil
}

// settingsJSONHooks builds the "hooks" object claude and gemini both read
// out of a settings.json: event name -> matcher groups -> hook commands.
func settingsJSONHooks(events []string, handlerCmd string) (string, error) {
	hooksObj := map[string]any{}
	for _, ev := range events {
		hooksObj[ev] = []any{map[string]any{
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": handlerCmd,
			}},
		}}
	}
	return marshalIndent(map[string]any{"hooks": hooksObj})
}

// codexHooksJSON builds the whole hooks.json document codex's
// hook_config_paths points at.
func codexHooksJSON(events []string, handlerCmd string) (string, error) {
	hooksObj := map[string]any{}
	for _, ev := range events {
		hooksObj[ev] = []any{map[string]any{
			"type":    "command",
			"command": handlerCmd,
		}}
	}
	return marshalIndent(map[string]any{"hooks": hooksObj})
}

// opencodePlugin builds the plugin module opencode's second
// hook_config_paths entry (.opencode/plugins/nerv-bridge.ts) is shaped
// like: a default-exported factory returning one callback per dotted event
// name.
func opencodePlugin(events []string, argv []string) string {
	bin, args := "axon", []string{}
	if len(argv) > 0 {
		bin, args = argv[0], argv[1:]
	}
	var b strings.Builder
	b.WriteString("// Capture-only OpenCode plugin: records each envelope and returns\n")
	b.WriteString("// nothing. An OpenCode callback blocks by raising, so every path\n")
	b.WriteString("// below is wrapped and every error is swallowed.\n")
	b.WriteString("import { spawn } from \"node:child_process\";\n\n")
	b.WriteString("const record = (type: string, event: unknown) => {\n")
	b.WriteString("  try {\n")
	fmt.Fprintf(&b, "    const child = spawn(%q, %s, {\n", bin, jsArray(args))
	b.WriteString("      stdio: [\"pipe\", \"ignore\", \"ignore\"],\n")
	b.WriteString("      detached: false,\n")
	b.WriteString("    });\n")
	b.WriteString("    child.on(\"error\", () => {});\n")
	b.WriteString("    child.stdin?.on(\"error\", () => {});\n")
	b.WriteString("    child.stdin?.end(JSON.stringify({ ...(event as object), type }));\n")
	b.WriteString("  } catch {\n")
	b.WriteString("    // Recording must never affect the session.\n")
	b.WriteString("  }\n")
	b.WriteString("};\n\n")
	b.WriteString("export default () => ({\n")
	for _, ev := range events {
		fmt.Fprintf(&b, "  %q: async (event: unknown) => record(%q, event),\n", ev, ev)
	}
	b.WriteString("});\n")
	return b.String()
}

// shellJoin renders an argv as one command line for the hosts whose
// settings file holds a "command" STRING that the host itself splits.
// Every word that is not plainly safe is single-quoted, so a path with a
// space survives and a path with a shell metacharacter cannot start a
// second command.
func shellJoin(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		out[i] = shellQuote(a)
	}
	return strings.Join(out, " ")
}

// shellQuote single-quotes a word unless every byte in it is one the shell
// leaves alone. An embedded single quote is closed, escaped and reopened,
// which is the only way to carry one: there is no escape for a single
// quote inside single quotes.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool { return !shellSafe(r) }) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shellSafe reports whether r needs no quoting in a shell word.
func shellSafe(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	default:
		return strings.ContainsRune("@%_-+=:,./", r)
	}
}

// jsArray renders a string slice as a JS array literal, which is the
// second parameter node:child_process spawn takes.
func jsArray(items []string) string {
	quoted := make([]string, len(items))
	for i, it := range items {
		quoted[i] = fmt.Sprintf("%q", it)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func marshalIndent(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return "", fmt.Errorf("capture: %w", err)
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

// HandlerEvents returns the host-side event names a handler installed for
// host would be invoked for, given the same all flag SnippetFor uses. It
// exists so a caller can report the subscription without rebuilding the
// snippet body.
func HandlerEvents(host string, all bool) ([]string, error) {
	s, err := SnippetFor(host, []string{"axon", "capture", "handle", host}, all)
	if err != nil {
		return nil, err
	}
	return s.HostEvents, nil
}
