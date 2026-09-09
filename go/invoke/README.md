# invoke

## What it answers

How to turn one normalized `Invocation` into native argv for a given
agent CLI, with every shim or refusal named before anything runs. Which
hosts are known, and where their session stores live, is the `axon`
root package (`axon.Get`, `axon.Hosts`).

## Use it when

- you launch an agent CLI from a tool: pick the adapter under `adapters/<cli>`, call `Build(inv)`, exec the `CommandSpec`
- you must explain degradation to the user first: inspect `Diagnostics` (`HasErrors`, `Errors`, `Filter(level)`)
- you want a one-shot build-and-exec: implement or wire a `Runner`
- you render a parity table: `Mappings()` and `ToolCapabilities()` on any adapter, or read the generated `spec/hosts/<host>/invoke.yaml`
- you need every built-in adapter at once: `invoke/all.Adapters()`

## Quick start

```go
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
```

## Contract

- `Build` is pure: no exec, no filesystem writes. `CommandSpec.Args` excludes `Path`, matching `os/exec.Cmd.Args[1:]`.
- Every universal option maps as `native`, `shim`, `unsupported` or `dangerous`. Shims emit a warning diagnostic; unsupported options requested by the caller make `Build` return an error; dangerous mappings are refused unless `Config["uxp.allow_dangerous"] = "true"`.
- Anti-shims: `ApprovalAutoEdit` never degrades to a target's auto-all flag, `Fork` is never emulated by resume plus fresh session, `Sandbox*` never cross-shims to container isolation.
- `ModeResume` needs `SessionID` or `Continue`. `Config` keys are `<cli>.<key>` for one adapter and `uxp.<key>` across adapters; unknown keys yield an info diagnostic.
- `OutputJSON` is one final-message object; `OutputStreamJSON` is the CLI's native event stream.

## Universal-option parity

The matrix below is auto-generated from each adapter's `Mappings()`
slice via `internal/gen/invoke`. Run `go generate ./...` to
regenerate; `make generate-check` fails on a non-empty diff.

<!-- parity:start -->
| Universal | claude | codex | copilot | crush | cursor-agent | gemini | goose | kimi | opencode | qwen | vibe |
|---|---|---|---|---|---|---|---|---|---|---|---|
| `ModeRun` | N | N | N | N | N | N | N | N | N | N | N |
| `ModeInteractive` | N | N | N | N | N | N | N | N | N | N | N |
| `ModeResume` | N | N | N | N | N | N | N | N | N | N | N |
| `Continue` | N | N | N | N | N | N | N | N | N | N | N |
| `Fork` | N | N | U | U | U | U | N | U | N | U | U |
| `CWD` | N | N | N | N | N | N | N | N | N | N | N |
| `Model` | N | N | N | N | N | N | N | N | N | N | U |
| `Agent` | N | U | N | U | U | U | S | N | N | U | N |
| `OutputText` | N | N | N | N | N | N | N | N | N | N | N |
| `OutputJSON` | N | S | S | U | N | N | N | S | S | N | N |
| `OutputStreamJSON` | N | N | N | U | N | N | N | N | N | N | N |
| `SandboxReadOnly` | S | N | U | U | U | S | U | U | U | S | U |
| `SandboxWorkspaceWrite` | S | N | U | U | U | S | U | U | U | S | U |
| `SandboxDangerFullAccess` | D | D | D | D | D | D | U | U | D | D | U |
| `ApprovalAsk` | N | N | N | N | N | N | N | N | N | N | N |
| `ApprovalPlan` | N | S | U | U | U | N | U | N | U | N | S |
| `ApprovalAutoEdit` | N | U | U | U | U | N | U | U | U | N | S |
| `ApprovalAutoAll` | D | D | D | D | D | D | U | D | D | D | D |
| `ApprovalNever` | S | N | U | U | U | U | U | U | U | U | U |
| `AddDirs` | N | N | N | S | S | N | S | N | S | N | S |
| `Files` | S | S | S | S | S | S | S | S | N | S | S |
| `Images` | S | N | S | S | S | S | S | S | S | S | S |

Legend: `N` native · `S` shim · `U` unsupported · `D` dangerous (opt-in required).
<!-- parity:end -->

## Tool capability parity

<!-- tools:start -->
| Tool | claude | codex | copilot | crush | cursor-agent | gemini | goose | kimi | opencode | qwen | vibe |
|---|---|---|---|---|---|---|---|---|---|---|---|
| `shell.exec` | N | N | N | N | N | N | N | N | N | N | N |
| `file.read` | N | S | N | N | N | N | N | N | N | N | N |
| `file.write` | N | S | N | N | N | N | N | N | N | N | N |
| `file.edit` | N | N | N | N | N | N | N | N | N | N | N |
| `file.search` | N | S | N | S | S | N | S | S | N | N | S |
| `web.search` | N | N | S | U | U | N | S | S | S | S | U |
| `web.fetch` | N | S | N | U | U | N | S | S | N | S | U |
| `todo.write` | N | U | U | U | U | S | U | U | N | U | U |
| `task.spawn` | N | U | U | U | U | S | U | U | N | U | U |
| `plan.update` | U | N | U | U | U | U | U | U | U | U | S |
| `mcp.call` | N | N | N | S | N | N | N | N | N | N | U |
| `image.read` | S | N | U | U | U | N | U | U | S | S | U |
| `browser.operate` | S | U | U | U | U | U | U | U | U | U | U |
| `user.message` | N | U | N | U | U | U | U | N | U | U | U |

Legend: `N` native · `S` shim · `U` unsupported.
<!-- tools:end -->

## Adapters

One package per CLI under `adapters/<name>`: claude, codex, copilot,
crush, cursoragent, gemini, goose, kimi, opencode, qwen, vibe. Each
adapter README documents the per-CLI flag mapping, shim inventory,
anti-shim refusals and recognized `Config` keys.

## Shims

Six closed-set shims live in `invoke/shim/`. Adapters do not invent
new shims; the catalog is fixed in spec §15.5.

| Shim | Helper | Used by |
|---|---|---|
| S-1 (parent-dir reduce) | `ExpandToParentDirs` | gemini, codex, copilot, qwen, kimi |
| S-2 (enumerate dir → files) | `EnumerateDirFiles` | opencode |
| S-3 (prompt-block) | `FormatFileBlock` | every adapter without native scoping |
| S-4 (builtin-agent → approval) | inline | vibe |
| S-5 (recipe ↔ agent) | inline | goose |
| S-6 (sandbox/approval cross-shim) | inline | codex |

## Universal `Config` keys

Per-adapter Config keys use the `<cli>.<key>` namespace. Two
cross-adapter keys live under `uxp.`:

| Key | Type | Effect |
|---|---|---|
| `uxp.allow_dangerous` | bool | Required to enable any `MappingDangerous` mapping (e.g. `--yolo`, `--dangerously-skip-permissions`). |
| `uxp.shim.dir_to_files_max` | int | S-2 enumeration cap. Default 200; overflow is a hard error. |

## Neighbours

- [`adapters/`](adapters/README.md): one package per CLI.
- [`shim/`](shim/README.md): the closed catalog of mapping helpers adapters share.
- `all/`: `Adapters()`, every built-in adapter sorted by CLI name.
- `../internal/gen/invoke`: the generator behind this page and `spec/hosts/<host>/invoke.yaml`.

## See also

- `spec/invoke.schema.json`: the schema the generated `invoke.yaml` files validate against.
