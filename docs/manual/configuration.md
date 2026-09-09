# Configuration reference: axon

## Flags

Every command accepts:

| Flag | Default | Description |
|---|---|---|
| `-f`, `--format` | `text` | Output format: `text`, `json`, or `yaml` |
| `-v`, `--verbose` | `false` | Verbose output |

## Config file and environment

The CLI wires a config file at `~/.config/axon/config.yaml` and reads
environment variables prefixed `AXON_` (via viper), but no command
currently reads a key from either source: every setting today is a flag.
The wiring is in place for future settings; nothing to configure yet
beyond the flags above.
