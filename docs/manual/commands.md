# Commands reference: axon

## Global flags

```
-f, --format string   Output format (text, json, yaml) (default "text")
-h, --help             help for axon
-v, --verbose          Verbose output
    --version          version for axon
```

## Commands

### `hosts`

List every registered host.

```sh
axon hosts
```

### `hosts show <name>`

Print one host's full record (identity, hook config paths, exit codes).

```sh
axon hosts show claude
```

### `events`

List the canonical hook event catalog (name, origin, whether it blocks).

```sh
axon events
```

### `fixture <host> <event>`

Print a default hook `Input`, encoded in that host's native wire shape.

```sh
axon fixture claude PreToolUse
```

### `validate input <host> <event> <file|->`

Validate a hook input payload against the host's JSON Schema for that
event. Reads stdin when the file argument is `-`.

### `validate decision <host> <event> <action> <file|->`

Validate a hook decision payload (`action` is one of `allow`, `warn`,
`block`, `rewrite`) against the host's schema for that event and action.

### `invoke <host> [-- <prompt words...>]`

Build native argv for a host agent CLI from one normalized request, or
spawn it directly with `--exec`. See `axon invoke --help` for the full
flag set (`--model`, `--sandbox`, `--approval`, `--config`, and more).

```sh
axon invoke claude -- "explain this diff"
```

### `completion`

Generate a shell autocompletion script (cobra default).

## Exit codes

0 success, 1 runtime error, 2 usage error (unknown host, event, action,
or bad arguments; an unrecognized subcommand or flag; wrong arg count).
