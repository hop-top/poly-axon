# Troubleshooting axon

## `unknown host "<name>"`

The name is not a registered host. Run `axon hosts` for the canonical
list, or pass one of the published aliases (`claude-code`, `gemini-cli`,
`codex-cli`). This is a usage error: exit code 2.

## `axon/hooks: schema: ...`

`validate input` or `validate decision` failed against the host's JSON
Schema for that event. The message names the schema file and the missing
or invalid fields. Compare your payload against
`axon fixture <host> <event>`, which prints a payload that validates.

## Command not found

Ensure `axon` is installed and on `$PATH`:

```sh
which axon
```

See [install](install.md), or run `make symlink` from a source checkout.

## Getting help

1. Check this page and [`docs/adding-a-host.md`](../adding-a-host.md) if
   the issue involves a specific host.
2. Search existing issues on GitHub.
3. Open a new issue with `axon --version`, the exact command, and the
   full output.
