# Getting started with axon

## As a library

```go
import "hop.top/axon"

host, ok := axon.Resolve("claude-code") // aliases resolve too
hosts := axon.Hosts()                   // all 17 registered hosts
```

## As a CLI

```sh
axon hosts              # list every registered host
axon hosts show claude   # one host's full record
axon events              # the canonical hook event catalog
```

## Next steps

- [Commands reference](commands.md)
- [Configuration reference](configuration.md)
- [Adding a host](../adding-a-host.md)
- [Troubleshooting](troubleshooting.md)
