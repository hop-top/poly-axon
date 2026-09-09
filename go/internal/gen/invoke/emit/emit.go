// Package emit renders the invoke-adapter parity README and the
// per-host invoke.yaml spec files from a live set of adapters. It is
// a library so both the generator (internal/gen/invoke) and the
// parity test (invoke/parity_test.go) can call the same code and
// compare against committed output.
package emit

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"hop.top/axon/invoke"
)

// universalOptions enumerates every option name an adapter's
// Mappings() must cover, in spec §15.4 order.
var universalOptions = []string{
	"ModeRun", "ModeInteractive", "ModeResume", "Continue", "Fork",
	"CWD", "Model", "Agent",
	"OutputText", "OutputJSON", "OutputStreamJSON",
	"SandboxReadOnly", "SandboxWorkspaceWrite", "SandboxDangerFullAccess",
	"ApprovalAsk", "ApprovalPlan", "ApprovalAutoEdit", "ApprovalAutoAll", "ApprovalNever",
	"AddDirs", "Files", "Images",
}

// universalToolCapabilities are the ToolCapability slots adapters
// must populate, per spec §8.
var universalToolCapabilities = []string{
	"shell.exec", "file.read", "file.write", "file.edit", "file.search",
	"web.search", "web.fetch", "todo.write", "task.spawn", "plan.update",
	"mcp.call", "image.read", "browser.operate", "user.message",
}

// Render produces the parity README and one invoke.yaml document per
// adapter from the given adapter set. adapters need not be sorted;
// Render sorts a local copy by CLI() for deterministic output.
func Render(adapters []invoke.InvocationAdapter) (readme []byte, yamlByHost map[string][]byte, err error) {
	ordered := make([]invoke.InvocationAdapter, len(adapters))
	copy(ordered, adapters)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].CLI() < ordered[j].CLI() })

	yamlByHost = make(map[string][]byte, len(ordered))
	for _, a := range ordered {
		b, err := renderHostYAML(a)
		if err != nil {
			return nil, nil, fmt.Errorf("render invoke.yaml for %s: %w", a.CLI(), err)
		}
		yamlByHost[a.CLI()] = b
	}

	readme = []byte(renderREADME(ordered))
	return readme, yamlByHost, nil
}

// renderHostYAML marshals {host, mappings, tools} to JSON first (so
// field names come from Go's default json encoding of the exported
// struct fields — there are no json tags on invoke.OptionMapping or
// invoke.ToolCapability) then decodes into a map[string]any before
// handing to yaml.v3. This keeps YAML field names identical to
// whatever json.Marshal would emit, rather than to Go field names
// directly, so a future json-tag addition changes both wire formats
// in lockstep.
func renderHostYAML(a invoke.InvocationAdapter) ([]byte, error) {
	payload := struct {
		Host     string                  `json:"host"`
		Mappings []invoke.OptionMapping  `json:"mappings"`
		Tools    []invoke.ToolCapability `json:"tools"`
	}{
		Host:     a.CLI(),
		Mappings: a.Mappings(),
		Tools:    a.ToolCapabilities(),
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}

	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

func renderREADME(ordered []invoke.InvocationAdapter) string {
	var b strings.Builder

	b.WriteString(readmeHeader)
	b.WriteString("\n## Universal-option parity\n\n")
	b.WriteString("The matrix below is auto-generated from each adapter's `Mappings()`\n")
	b.WriteString("slice via `internal/gen/invoke`. Run `go generate ./...` to\n")
	b.WriteString("regenerate; `make generate-check` fails on a non-empty diff.\n\n")
	b.WriteString("<!-- parity:start -->\n")
	b.WriteString(renderParityTable(ordered))
	b.WriteString("\n<!-- parity:end -->\n")

	b.WriteString("\n## Tool capability parity\n\n")
	b.WriteString("<!-- tools:start -->\n")
	b.WriteString(renderToolsTable(ordered))
	b.WriteString("\n<!-- tools:end -->\n")

	b.WriteString("\n")
	b.WriteString(readmeFooter)

	return b.String()
}

const readmeHeader = `# invoke

## What it answers

How to turn one normalized ` + "`Invocation`" + ` into native argv for a given
agent CLI, with every shim or refusal named before anything runs. Which
hosts are known, and where their session stores live, is the ` + "`axon`" + `
root package (` + "`axon.Get`" + `, ` + "`axon.Hosts`" + `).

## Use it when

- you launch an agent CLI from a tool: pick the adapter under ` + "`adapters/<cli>`" + `, call ` + "`Build(inv)`" + `, exec the ` + "`CommandSpec`" + `
- you must explain degradation to the user first: inspect ` + "`Diagnostics`" + ` (` + "`HasErrors`, `Errors`, `Filter(level)`" + `)
- you want a one-shot build-and-exec: implement or wire a ` + "`Runner`" + `
- you render a parity table: ` + "`Mappings()`" + ` and ` + "`ToolCapabilities()`" + ` on any adapter, or read the generated ` + "`spec/hosts/<host>/invoke.yaml`" + `
- you need every built-in adapter at once: ` + "`invoke/all.Adapters()`" + `

## Quick start

` + "```go" + `
spec, ds, err := claude.New().Build(invoke.Invocation{
    CLI:    axon.HostClaude,
    Mode:   invoke.ModeRun,
    Prompt: "summarize this repo",
})
if err != nil || ds.HasErrors() {
    fmt.Println("refused:", err, ds.Errors())
    return
}
fmt.Println(spec.Path, spec.Args)
// Output: claude [-p summarize this repo]
` + "```" + `

## Contract

- ` + "`Build`" + ` is pure: no exec, no filesystem writes. ` + "`CommandSpec.Args`" + ` excludes ` + "`Path`" + `, matching ` + "`os/exec.Cmd.Args[1:]`" + `.
- Every universal option maps as ` + "`native`, `shim`, `unsupported` or `dangerous`" + `. Shims emit a warning diagnostic; unsupported options requested by the caller make ` + "`Build`" + ` return an error; dangerous mappings are refused unless ` + "`Config[\"uxp.allow_dangerous\"] = \"true\"`" + `.
- Anti-shims: ` + "`ApprovalAutoEdit`" + ` never degrades to a target's auto-all flag, ` + "`Fork`" + ` is never emulated by resume plus fresh session, ` + "`Sandbox*`" + ` never cross-shims to container isolation.
- ` + "`ModeResume`" + ` needs ` + "`SessionID`" + ` or ` + "`Continue`" + `. ` + "`Config`" + ` keys are ` + "`<cli>.<key>`" + ` for one adapter and ` + "`uxp.<key>`" + ` across adapters; unknown keys yield an info diagnostic.
- ` + "`OutputJSON`" + ` is one final-message object; ` + "`OutputStreamJSON`" + ` is the CLI's native event stream.
`

const readmeFooter = `## Adapters

One package per CLI under ` + "`adapters/<name>`" + `: claude, codex, copilot,
crush, cursoragent, gemini, goose, kimi, opencode, qwen, vibe. Each
adapter README documents the per-CLI flag mapping, shim inventory,
anti-shim refusals and recognized ` + "`Config`" + ` keys.

## Shims

Six closed-set shims live in ` + "`invoke/shim/`" + `. Adapters do not invent
new shims; the catalog is fixed in spec §15.5.

| Shim | Helper | Used by |
|---|---|---|
| S-1 (parent-dir reduce) | ` + "`ExpandToParentDirs`" + ` | gemini, codex, copilot, qwen, kimi |
| S-2 (enumerate dir → files) | ` + "`EnumerateDirFiles`" + ` | opencode |
| S-3 (prompt-block) | ` + "`FormatFileBlock`" + ` | every adapter without native scoping |
| S-4 (builtin-agent → approval) | inline | vibe |
| S-5 (recipe ↔ agent) | inline | goose |
| S-6 (sandbox/approval cross-shim) | inline | codex |

## Universal ` + "`Config`" + ` keys

Per-adapter Config keys use the ` + "`<cli>.<key>`" + ` namespace. Two
cross-adapter keys live under ` + "`uxp.`" + `:

| Key | Type | Effect |
|---|---|---|
| ` + "`uxp.allow_dangerous`" + ` | bool | Required to enable any ` + "`MappingDangerous`" + ` mapping (e.g. ` + "`--yolo`, `--dangerously-skip-permissions`" + `). |
| ` + "`uxp.shim.dir_to_files_max`" + ` | int | S-2 enumeration cap. Default 200; overflow is a hard error. |

## Neighbours

- [` + "`adapters/`" + `](adapters/README.md): one package per CLI.
- [` + "`shim/`" + `](shim/README.md): the closed catalog of mapping helpers adapters share.
- ` + "`all/`" + `: ` + "`Adapters()`" + `, every built-in adapter sorted by CLI name.
- ` + "`../internal/gen/invoke`" + `: the generator behind this page and ` + "`spec/hosts/<host>/invoke.yaml`" + `.

## See also

- ` + "`spec/invoke.schema.json`" + `: the schema the generated ` + "`invoke.yaml`" + ` files validate against.
`

func renderParityTable(ordered []invoke.InvocationAdapter) string {
	var b strings.Builder

	fmt.Fprintf(&b, "| Universal |")
	for _, a := range ordered {
		fmt.Fprintf(&b, " %s |", a.CLI())
	}
	b.WriteString("\n|---|")
	for range ordered {
		b.WriteString("---|")
	}
	b.WriteString("\n")

	mappingsByCLI := map[string]map[string]invoke.OptionMapping{}
	for _, a := range ordered {
		mappingsByCLI[a.CLI()] = map[string]invoke.OptionMapping{}
		for _, m := range a.Mappings() {
			mappingsByCLI[a.CLI()][m.Universal] = m
		}
	}

	for _, opt := range universalOptions {
		fmt.Fprintf(&b, "| `%s` |", opt)
		for _, a := range ordered {
			cell := mappingsByCLI[a.CLI()][opt]
			fmt.Fprintf(&b, " %s |", supportSymbol(cell.Support))
		}
		b.WriteString("\n")
	}

	b.WriteString("\nLegend: `N` native · `S` shim · `U` unsupported · `D` dangerous (opt-in required).\n")
	return strings.TrimRight(b.String(), "\n")
}

func renderToolsTable(ordered []invoke.InvocationAdapter) string {
	var b strings.Builder

	fmt.Fprintf(&b, "| Tool |")
	for _, a := range ordered {
		fmt.Fprintf(&b, " %s |", a.CLI())
	}
	b.WriteString("\n|---|")
	for range ordered {
		b.WriteString("---|")
	}
	b.WriteString("\n")

	capsByCLI := map[string]map[string]invoke.ToolCapability{}
	for _, a := range ordered {
		capsByCLI[a.CLI()] = map[string]invoke.ToolCapability{}
		for _, c := range a.ToolCapabilities() {
			capsByCLI[a.CLI()][c.Universal] = c
		}
	}

	for _, tool := range universalToolCapabilities {
		fmt.Fprintf(&b, "| `%s` |", tool)
		for _, a := range ordered {
			cell := capsByCLI[a.CLI()][tool]
			fmt.Fprintf(&b, " %s |", supportSymbol(cell.Support))
		}
		b.WriteString("\n")
	}

	b.WriteString("\nLegend: `N` native · `S` shim · `U` unsupported.\n")
	return strings.TrimRight(b.String(), "\n")
}

func supportSymbol(s invoke.MappingSupport) string {
	switch s {
	case invoke.MappingNative:
		return "N"
	case invoke.MappingShim:
		return "S"
	case invoke.MappingDangerous:
		return "D"
	case invoke.MappingUnsupported:
		return "U"
	}
	return "?"
}
