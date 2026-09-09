package cmd_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/axon"
	"hop.top/axon/hooks"
	// Registers the four host codecs, so hooks.Registered() is the same
	// set the CLI itself sees.
	_ "hop.top/axon/hooks/hosts"
)

var binPath string

func TestMain(m *testing.M) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		panic(err)
	}
	binPath = filepath.Join(repoRoot, "bin", "axon-test")

	build := exec.Command("go", "build", "-buildvcs=false", "-o", binPath, "./cmd/axon") //nolint:gosec // fixed test-build invocation, no external input
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		panic("build axon-test failed: " + err.Error() + "\n" + string(out))
	}

	code := m.Run()

	_ = os.Remove(binPath)
	os.Exit(code)
}

type result struct {
	stdout string
	stderr string
	code   int
}

func run(t *testing.T, args ...string) result {
	t.Helper()
	cmd := exec.Command(binPath, args...) //nolint:gosec // invokes the test-built axon binary with test-supplied args
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("run %v: %v", args, err)
		}
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func TestHostsListsSeventeenRows(t *testing.T) {
	r := run(t, "hosts")
	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", r.code, r.stderr)
	}
	lines := strings.Split(strings.TrimRight(r.stdout, "\n"), "\n")
	// Header row + 17 host rows.
	dataRows := len(lines) - 1
	if dataRows != 17 {
		t.Fatalf("got %d data rows, want 17; output:\n%s", dataRows, r.stdout)
	}
}

func TestHostsShowResolvesAlias(t *testing.T) {
	r := run(t, "hosts", "show", "claude-code")
	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "name: claude") {
		t.Fatalf("stdout = %q, want it to contain %q", r.stdout, "name: claude")
	}
}

func TestFixtureOutputValidatesAgainstInputSchema(t *testing.T) {
	r := run(t, "fixture", "claude", "PreToolUse")
	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", r.code, r.stderr)
	}
	if err := hooks.ValidateInput("claude", "PreToolUse", []byte(r.stdout)); err != nil {
		t.Fatalf("fixture output failed schema validation: %v\noutput: %s", err, r.stdout)
	}
}

func TestValidateDecisionExitCodes(t *testing.T) {
	fixture := filepath.Join("..", "spec", "fixtures", "hosts", "gemini", "PreToolUse.block.json")
	r := run(t, "validate", "decision", "gemini", "PreToolUse", "block", fixture)
	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", r.code, r.stderr)
	}

	tmp, err := os.CreateTemp(t.TempDir(), "empty-*.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmp.WriteString("{}"); err != nil {
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}

	r = run(t, "validate", "decision", "gemini", "PreToolUse", "block", tmp.Name())
	if r.code != 1 {
		t.Fatalf("exit code = %d, want 1; stdout=%s stderr=%s", r.code, r.stdout, r.stderr)
	}
	if r.stderr == "" {
		t.Fatal("expected error message on stderr")
	}
}

func TestFixtureUnknownHostExitsUsage(t *testing.T) {
	r := run(t, "fixture", "nope", "PreToolUse")
	if r.code != 2 {
		t.Fatalf("exit code = %d, want 2; stdout=%s stderr=%s", r.code, r.stdout, r.stderr)
	}
	want := `unknown host "nope"`
	if !strings.Contains(r.stderr, want) {
		t.Fatalf("stderr = %q, want it to contain %q", r.stderr, want)
	}
}

func TestBogusCommandExitsUsage(t *testing.T) {
	r := run(t, "bogus")
	if r.code != 2 {
		t.Fatalf("exit code = %d, want 2; stdout=%s stderr=%s", r.code, r.stdout, r.stderr)
	}
}

func TestEventsListsRows(t *testing.T) {
	r := run(t, "events")
	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", r.code, r.stderr)
	}
	lines := strings.Split(strings.TrimRight(r.stdout, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected header + rows, got: %s", r.stdout)
	}
}

func TestHostsJSONFormat(t *testing.T) {
	r := run(t, "hosts", "--format", "json")
	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", r.code, r.stderr)
	}
	if !strings.HasPrefix(strings.TrimSpace(r.stdout), "[") {
		t.Fatalf("expected JSON array, got: %s", r.stdout)
	}
}

func TestValidateInputStdin(t *testing.T) {
	fixture := filepath.Join("..", "spec", "fixtures", "hosts", "claude", "PreToolUse.input.json")
	data, err := os.ReadFile(fixture) //nolint:gosec // fixed test fixture path built from literal constants
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binPath, "validate", "input", "claude", "PreToolUse", "-")
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("validate input via stdin failed: %v; stderr=%s", err, stderr.String())
	}
}

func TestInvokeUnknownHostExitsUsage(t *testing.T) {
	r := run(t, "invoke", "nope", "--", "hello")
	if r.code != 2 {
		t.Fatalf("exit code = %d, want 2; stdout=%s stderr=%s", r.code, r.stdout, r.stderr)
	}
}

// TestFixtureOutputValidatesForEveryEmittedPair closes the gap that let
// `axon fixture`'s output ship unvalidatable: for EVERY host/event pair the
// CLI will emit for, the emitted envelope must pass `axon validate input`
// for that same pair. Both are CLI-only surfaces — no shared-library
// conformance check or parity case tied them together, so 41 of 53 pairs
// emitted output their own host schema rejected.
//
// The pair set is derived from the spec, not hardcoded: every canonical
// event a hooked host declares under native: or close: in its
// capabilities.yaml. Adding a capability row, a codec, or a hook schema
// therefore extends this test automatically.
//
// A pair splits two ways, and each way is asserted:
//   - a host input schema exists  -> fixture MUST succeed and its output
//     MUST validate against that schema.
//   - no input schema exists      -> fixture MUST refuse with a non-zero
//     exit and print nothing on stdout. Capability data ships ahead of
//     captured envelopes on purpose (docs/spec-contract-notes.md §4,
//     hooks.fixtureDeferred), and a fixture nothing has verified must fail
//     loudly rather than emit an envelope validate would then reject.
func TestFixtureOutputValidatesForEveryEmittedPair(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	emitted, refused := 0, 0
	for _, name := range hooks.Registered() {
		caps, err := hooks.LoadCapabilities(name)
		if err != nil {
			t.Fatalf("%s: LoadCapabilities: %v", name, err)
		}
		seen := map[axon.Event]bool{}
		for _, p := range append(append([]hooks.Pair{}, caps.Native...), caps.Close...) {
			if seen[p.Event] {
				continue
			}
			seen[p.Event] = true
			ev := p.Event
			t.Run(name+"/"+string(ev), func(t *testing.T) {
				schema := filepath.Join(repoRoot, "spec", "hosts", name, "hooks", string(ev)+".input.schema.json")
				_, statErr := os.Stat(schema)
				hasSchema := statErr == nil

				r := run(t, "fixture", name, string(ev))
				if !hasSchema {
					refused++
					if r.code == 0 {
						t.Fatalf("fixture %s %s: exit 0 with no schema at %s; an unverifiable envelope must be refused, not emitted\nstdout=%s",
							name, ev, schema, r.stdout)
					}
					if strings.TrimSpace(r.stdout) != "" {
						t.Errorf("fixture %s %s: refused but still wrote stdout=%q", name, ev, r.stdout)
					}
					return
				}
				emitted++
				if r.code != 0 {
					t.Fatalf("fixture %s %s: exit %d, want 0 (schema exists at %s); stderr=%s",
						name, ev, r.code, schema, r.stderr)
				}
				payload := strings.TrimSpace(r.stdout)
				if payload == "" {
					t.Fatalf("fixture %s %s: exit 0 but empty stdout", name, ev)
				}
				// The real gate: the CLI's own validate surface, run
				// exactly as a user would pipe it.
				cmd := exec.Command(binPath, "validate", "input", name, string(ev), "-") //nolint:gosec // test-built binary, spec-derived args
				cmd.Stdin = strings.NewReader(payload)
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				if err := cmd.Run(); err != nil {
					t.Errorf("`axon fixture %s %s` output fails `axon validate input %s %s`:\n%s\npayload: %s",
						name, ev, name, ev, strings.TrimSpace(stderr.String()), payload)
				}
			})
		}
	}
	if emitted == 0 || refused == 0 {
		t.Errorf("expected both emitting and refusing pairs; got emitted=%d refused=%d", emitted, refused)
	}
}
