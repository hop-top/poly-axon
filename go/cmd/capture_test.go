package cmd_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/axon"
	"hop.top/axon/hooks"
	_ "hop.top/axon/hooks/hosts"
)

// TestCaptureHandleReproducesAFixtureEndToEnd is the whole tool exercised
// without a live host: a committed fixture is piped to the handler exactly
// as a host would pipe an envelope, and the file that lands must be
// correctly named and byte-identical to the fixture.
func TestCaptureHandleReproducesAFixtureEndToEnd(t *testing.T) {
	dir := t.TempDir()
	want := specFile(t, "fixtures/hosts/claude/Stop.input.json")

	r := runStdin(t, string(want), "capture", "handle", "claude", "--dir", dir)
	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", r.code, r.stderr)
	}
	// A hook handler's stdout is read by the host as a decision. claude
	// has no decision channel for Stop, so the handler must say nothing.
	if r.stdout != "" {
		t.Errorf("handler wrote %q to stdout; claude Stop has no decision channel, so it must stay silent", r.stdout)
	}

	got, err := os.ReadFile(filepath.Join(dir, "claude", "Stop.input.json")) //nolint:gosec // dir is this test's own t.TempDir()
	if err != nil {
		t.Fatalf("no capture landed: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("capture differs from the fixture it came from\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// TestCaptureHandleEmitsTheHostAllowShape: for a host and event that DO
// have a decision channel, the handler must emit that host's allow shape
// rather than silence, or the host reads an unset decision.
func TestCaptureHandleEmitsTheHostAllowShape(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		host    string
		fixture string
		want    string
	}{
		{"claude", "fixtures/hosts/claude/PreToolUse.input.json", "{}"},
		{"gemini", "fixtures/hosts/gemini/PreToolUse.input.json", `{"decision":"allow"}`},
		{"opencode", "fixtures/hosts/opencode/PreToolUse.input.json", `{"action":"allow"}`},
		{"codex", "fixtures/hosts/codex/PreToolUse.input.json", "{}"},
	} {
		t.Run(tc.host, func(t *testing.T) {
			r := runStdin(t, string(specFile(t, tc.fixture)), "capture", "handle", tc.host, "--dir", dir)
			if r.code != 0 {
				t.Fatalf("exit code = %d, want 0; stderr=%s", r.code, r.stderr)
			}
			if strings.TrimSpace(r.stdout) != tc.want {
				t.Errorf("stdout = %q, want %q", r.stdout, tc.want)
			}
		})
	}
}

// TestCaptureHandleNeverBlocksOnGarbage is the safety contract stated as
// a test: once the host is known, NOTHING that arrives on stdin can make
// the handler answer anything but allow. An unreadable stream, a
// non-JSON body, an envelope with no discriminator and one naming an
// event the host does not speak all have to end in the host's allow exit
// code with a decodable allow on stdout.
//
// The bug this pins: an unplaceable envelope used to surface as a usage
// error, and cobra exits 2 for those — which is claude's BLOCK code. A
// handler installed only to watch would have denied every tool call whose
// envelope it failed to parse.
func TestCaptureHandleNeverBlocksOnGarbage(t *testing.T) {
	dir := t.TempDir()
	for _, host := range []string{"claude", "codex", "gemini", "opencode"} {
		h, ok := axon.Get(host)
		if !ok {
			t.Fatalf("%s missing from the spec", host)
		}
		for _, payload := range []string{
			"",
			"not json",
			`{"session_id":"s"}`,
			`{"hook_event_name":"Nope"}`,
			`{"hook_event_name":"PreToolUse"`, // truncated
			`[]`,
		} {
			r := runStdin(t, payload, "capture", "handle", host, "--dir", dir)
			if r.code != h.ExitCodes.Allow {
				t.Errorf("%s: payload %q exited %d; host.yaml's allow code is %d",
					host, payload, r.code, h.ExitCodes.Allow)
			}
			if h.ExitCodes.Block != h.ExitCodes.Allow && r.code == h.ExitCodes.Block {
				t.Errorf("%s: payload %q exited with the BLOCK code %d", host, payload, h.ExitCodes.Block)
			}
			// Whatever it wrote must decode back to allow. An empty
			// stdout does, for every registered codec.
			if out := strings.TrimSpace(r.stdout); out != "" && !isAllowShape(host, out) {
				t.Errorf("%s: payload %q made the handler write %q, which is not that host's allow shape",
					host, payload, out)
			}
		}
	}
}

// isAllowShape reports whether out is the host's allow decision, checked
// by decoding it the way the runtime would.
func isAllowShape(host, out string) bool {
	c, ok := hooks.For(host)
	if !ok {
		return false
	}
	h, _ := axon.Get(host)
	d, err := c.DecodeDecision([]byte(out), h.ExitCodes.Allow)
	return err == nil && d.Action == hooks.ActionAllow
}

// TestCaptureHandleScrubsVolatileValues: a capture is committed to a
// public repo, so machine paths and live session ids must not survive it.
func TestCaptureHandleScrubsVolatileValues(t *testing.T) {
	dir := t.TempDir()
	live := `{"hook_event_name":"PreCompact","session_id":"7f3a1c92-55de-4b21-9e0c-2d8a44b17c05",` +
		`"cwd":"/Users/someone/private/repo","transcript_path":"/Users/someone/.claude/x.jsonl","trigger":"auto"}`
	r := runStdin(t, live, "capture", "handle", "claude", "--dir", dir)
	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", r.code, r.stderr)
	}
	got, err := os.ReadFile(filepath.Join(dir, "claude", "PreCompact.input.json")) //nolint:gosec // dir is this test's own t.TempDir()
	if err != nil {
		t.Fatalf("no capture landed: %v", err)
	}
	for _, leak := range []string{"7f3a1c92", "/Users/someone"} {
		if strings.Contains(string(got), leak) {
			t.Errorf("capture leaks %q:\n%s", leak, got)
		}
	}
	// The unrecognized key is the reason to capture at all, so it stays.
	if !strings.Contains(string(got), `"trigger"`) {
		t.Errorf("capture dropped the unrecognized key `trigger`:\n%s", got)
	}
}

// TestCaptureStatusNamesTheRefusedPairs: the report must name every pair
// `axon fixture` refuses, so an operator can see the work rather than
// discovering it one refusal at a time.
func TestCaptureStatusNamesTheRefusedPairs(t *testing.T) {
	dir := t.TempDir()
	r := run(t, "capture", "status", "--dir", dir, "--format", "json")
	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", r.code, r.stderr)
	}
	var pairs []struct {
		Host      string `json:"Host"`
		Event     string `json:"Event"`
		HasSchema bool   `json:"HasSchema"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &pairs); err != nil {
		t.Fatalf("status --format json is not JSON: %v\n%s", err, r.stdout)
	}
	refused := 0
	for _, p := range pairs {
		if !p.HasSchema {
			refused++
			// Cross-check against the CLI whose refusal this reports.
			if fr := run(t, "fixture", p.Host, p.Event); fr.code == 0 {
				t.Errorf("status says %s/%s is refused, but `axon fixture` accepted it", p.Host, p.Event)
			}
		}
	}
	if refused != 30 {
		t.Errorf("status reports %d refused pairs, want 30", refused)
	}
}

// TestCaptureStatusIsHostFilterable and human-readable.
func TestCaptureStatusFiltersByHost(t *testing.T) {
	dir := t.TempDir()
	r := run(t, "capture", "status", "gemini", "--dir", dir)
	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", r.code, r.stderr)
	}
	if strings.Contains(r.stdout, "\nclaude ") {
		t.Errorf("status gemini listed claude rows:\n%s", r.stdout)
	}
	if !strings.Contains(r.stdout, "BeforeModel") {
		t.Errorf("status gemini does not show the BeforeModel host event:\n%s", r.stdout)
	}
	// The report must say what to do next, not only what is true.
	if !strings.Contains(r.stdout, "Next:") {
		t.Errorf("status prints no next step:\n%s", r.stdout)
	}
}

// TestCaptureInstallPrintsConfigAndTouchesNothing: `install` prints; it
// must not create or modify a single file.
func TestCaptureInstallPrintsConfigAndTouchesNothing(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	before := treeSnapshot(t, home)

	r := run(t, "capture", "install", "claude", "--dir", dir)
	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", r.code, r.stderr)
	}
	for _, want := range []string{"~/.claude/settings.json", ".claude/settings.json", "capture handle claude", "```json"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("install output is missing %q:\n%s", want, r.stdout)
		}
	}
	if after := treeSnapshot(t, home); after != before {
		t.Errorf("install modified the filesystem: %q -> %q", before, after)
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) != 0 {
		t.Errorf("install created %d entries under the capture directory", len(entries))
	}
}

// TestCaptureInstallSnippetIsValidJSON: an operator pastes it into a
// settings file, so it must parse.
func TestCaptureInstallSnippetIsValidJSON(t *testing.T) {
	for _, host := range []string{"claude", "codex", "gemini"} {
		t.Run(host, func(t *testing.T) {
			r := run(t, "capture", "install", host, "--dir", t.TempDir())
			if r.code != 0 {
				t.Fatalf("exit code = %d; stderr=%s", r.code, r.stderr)
			}
			body := fencedBlock(t, r.stdout, "json")
			var doc map[string]any
			if err := json.Unmarshal([]byte(body), &doc); err != nil {
				t.Fatalf("snippet is not valid JSON: %v\n%s", err, body)
			}
			if _, ok := doc["hooks"]; !ok {
				t.Errorf("snippet has no \"hooks\" key:\n%s", body)
			}
		})
	}
}

// TestCapturePlanExitsZeroForEveryEvent: `plan` is the command an operator
// reads before trusting the handler, so every row must show a zero exit.
// A non-zero exit on any host event would be a handler that blocks.
func TestCapturePlanExitsZeroForEveryEvent(t *testing.T) {
	for _, host := range []string{"claude", "codex", "gemini", "opencode"} {
		t.Run(host, func(t *testing.T) {
			r := run(t, "capture", "plan", host)
			if r.code != 0 {
				t.Fatalf("exit code = %d, want 0; stderr=%s", r.code, r.stderr)
			}
			lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
			if len(lines) < 2 {
				t.Fatalf("plan printed no rows:\n%s", r.stdout)
			}
			for _, line := range lines[1:] {
				fields := strings.Fields(line)
				if len(fields) < 4 {
					t.Errorf("unparseable row %q", line)
					continue
				}
				// EVENT BLOCKING STDOUT EXIT DERIVED...; the exit column is
				// the one before the derivation text.
				if !strings.Contains(line, " 0 ") {
					t.Errorf("row does not show a zero exit code: %q", line)
				}
			}
		})
	}
}

// TestCaptureRefusesToWriteIntoSpec: the CLI must carry the package's
// guard, since --dir is where an operator could aim it.
func TestCaptureRefusesToWriteIntoSpec(t *testing.T) {
	specDir := filepath.Join(t.TempDir(), "spec", "fixtures", "hosts", "claude")
	if err := os.MkdirAll(specDir, 0o750); err != nil {
		t.Fatal(err)
	}
	r := runStdin(t, string(specFile(t, "fixtures/hosts/claude/Stop.input.json")),
		"capture", "handle", "claude", "--dir", specDir)
	if !strings.Contains(r.stderr, "refusing to write inside spec/") {
		t.Errorf("stderr does not report the refusal:\n%s", r.stderr)
	}
	if entries, _ := os.ReadDir(specDir); len(entries) != 0 {
		t.Errorf("a capture landed inside the spec directory anyway")
	}
}

// TestCaptureUnknownHostIsUsageError keeps the CLI's exit-code contract.
func TestCaptureUnknownHostIsUsageError(t *testing.T) {
	for _, args := range [][]string{
		{"capture", "install", "nope"},
		{"capture", "plan", "nope"},
		{"capture", "status", "nope"},
	} {
		r := run(t, args...)
		if r.code != 2 {
			t.Errorf("%v exit code = %d, want 2 (usage); stderr=%s", args, r.code, r.stderr)
		}
	}
}

// runStdin runs the test-built binary with stdin attached, which is how a
// host invokes a hook handler. cli_test.go's run() has no stdin, so this
// is its sibling rather than a replacement.
func runStdin(t *testing.T, stdin string, args ...string) result {
	t.Helper()
	cmd := exec.Command(binPath, args...) //nolint:gosec // invokes the test-built axon binary with test-supplied args
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		exitErr := &exec.ExitError{}
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("run %v: %v", args, err)
		}
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

// specFile reads one file out of the embedded spec tree.
func specFile(t *testing.T, p string) []byte {
	t.Helper()
	raw, err := fs.ReadFile(axon.Spec(), path.Clean(p))
	if err != nil {
		t.Fatalf("read spec/%s: %v", p, err)
	}
	return raw
}

// fencedBlock returns the body of the first ```<lang> fenced block.
func fencedBlock(t *testing.T, out, lang string) string {
	t.Helper()
	open := "```" + lang + "\n"
	i := strings.Index(out, open)
	if i < 0 {
		t.Fatalf("no ```%s block in:\n%s", lang, out)
	}
	rest := out[i+len(open):]
	j := strings.Index(rest, "\n```")
	if j < 0 {
		t.Fatalf("unterminated ```%s block in:\n%s", lang, out)
	}
	return rest[:j]
}

// treeSnapshot renders a directory tree as a sortable string, so a test
// can assert nothing under it changed.
func treeSnapshot(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		b.WriteString(p)
		b.WriteString("\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}
