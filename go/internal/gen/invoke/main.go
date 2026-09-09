// Command gen-invoke writes invoke/README.md (the parity tables) and
// spec/hosts/<host>/invoke.yaml (one per adapter) from the live
// invoke.InvocationAdapter set in invoke/all. The rendering itself
// lives in internal/gen/invoke/emit so invoke/parity_test.go can call
// the same code and compare against committed output without
// shelling out to `go run`.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"hop.top/axon/internal/gen/invoke/emit"
	"hop.top/axon/invoke/all"
)

func main() {
	readmeOut := flag.String("readme", "invoke/README.md", "path to the generated parity README")
	specDir := flag.String("spec-dir", "spec/hosts", "hosts spec directory")
	check := flag.Bool("check", false, "exit 1 if any output differs from disk")
	flag.Parse()

	readme, yamlByHost, err := emit.Render(all.Adapters())
	if err != nil {
		fatal(err)
	}

	if *check {
		ok := true
		if !fileMatches(*readmeOut, readme) {
			fmt.Fprintln(os.Stderr, *readmeOut+" is stale; run go generate ./...")
			ok = false
		}
		for host, data := range yamlByHost {
			path := filepath.Join(*specDir, host, "invoke.yaml")
			if !fileMatches(path, data) {
				fmt.Fprintln(os.Stderr, path+" is stale; run go generate ./...")
				ok = false
			}
		}
		if !ok {
			os.Exit(1)
		}
		return
	}

	if err := os.WriteFile(*readmeOut, readme, 0o644); err != nil { //nolint:gosec // generated Go source, meant to be world-readable like any other checked-in file
		fatal(err)
	}
	for host, data := range yamlByHost {
		dir := filepath.Join(*specDir, host)
		if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // generated spec directory, meant to be world-readable like the repo's checked-in tree
			fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "invoke.yaml"), data, 0o644); err != nil { //nolint:gosec // generated spec file, meant to be world-readable like the repo's checked-in tree
			fatal(err)
		}
	}
}

func fileMatches(path string, want []byte) bool {
	cur, err := os.ReadFile(path) //nolint:gosec // generator-controlled path (flag default joined with known host names)
	if err != nil {
		return false
	}
	return bytes.Equal(cur, want)
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "gen-invoke:", err); os.Exit(1) }
