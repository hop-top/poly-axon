package invoke_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"hop.top/axon"
	"hop.top/axon/internal/gen/invoke/emit"
	"hop.top/axon/invoke/all"
)

// TestParityIsUpToDate regenerates invoke/README.md and every
// spec/hosts/<host>/invoke.yaml in memory via emit.Render and
// compares byte-for-byte against what is committed. This is the
// in-process equivalent of `go run ./internal/gen/invoke -check`:
// it exercises the same emit.Render the generator's main.go calls,
// without shelling out.
//
// If this test fails, run:
//
//	go generate ./...
//
// and commit the diff.
func TestParityIsUpToDate(t *testing.T) {
	root := repoRoot(t)

	readme, yamlByHost, err := emit.Render(all.Adapters())
	if err != nil {
		t.Fatalf("emit.Render: %v", err)
	}

	wantReadme, err := os.ReadFile(filepath.Join(root, "invoke", "README.md")) //nolint:gosec // fixed checked-in file path, root is the repo root
	if err != nil {
		t.Fatalf("read invoke/README.md: %v", err)
	}
	if string(readme) != string(wantReadme) {
		t.Errorf("invoke/README.md is stale; run `go generate ./...` and commit the diff")
	}

	if len(yamlByHost) == 0 {
		t.Fatal("emit.Render returned no per-host YAML")
	}
	for host, data := range yamlByHost {
		path := filepath.Join(root, "spec", "hosts", host, "invoke.yaml")
		want, err := os.ReadFile(path) //nolint:gosec // path built from the repo root and compiled-in adapter names
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		if string(data) != string(want) {
			t.Errorf("%s is stale; run `go generate ./...` and commit the diff", path)
		}
	}
}

// repoRoot returns the module root, resolved via runtime.Caller so
// the test works regardless of the invoking directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// this file is <root>/invoke/parity_test.go
	return filepath.Dir(filepath.Dir(thisFile))
}

// TestEmittedInvokeYAMLUsesSnakeCaseKeys guards against Go field
// names leaking into spec/hosts/*/invoke.yaml: every later binding
// (TypeScript, Python) reads this file, so its keys must be
// snake_case, not Go's exported-field casing.
func TestEmittedInvokeYAMLUsesSnakeCaseKeys(t *testing.T) {
	_, yamlByHost, err := emit.Render(all.Adapters())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(yamlByHost[axon.HostClaude], &doc); err != nil {
		t.Fatal(err)
	}
	mappings, _ := doc["mappings"].([]any)
	if len(mappings) == 0 {
		t.Fatal("no mappings emitted for claude")
	}
	first, _ := mappings[0].(map[string]any)
	for key := range first {
		if key != strings.ToLower(key) {
			t.Errorf("mapping key %q is not snake_case; Go field names must not reach spec/", key)
		}
	}
	tools, _ := doc["tools"].([]any)
	if len(tools) == 0 {
		t.Fatal("no tools emitted for claude")
	}
	tool, _ := tools[0].(map[string]any)
	for key := range tool {
		if key != strings.ToLower(key) {
			t.Errorf("tool key %q is not snake_case; Go field names must not reach spec/", key)
		}
	}
}
