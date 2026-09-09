package capture_test

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/axon"
	"hop.top/axon/capture"
	"hop.top/axon/hooks"
	_ "hop.top/axon/hooks/hosts"
)

// TestWriteReproducesCommittedFixtures is the end-to-end proof with no
// live host in it: every committed fixture is fed to Write as if it had
// just arrived on a handler's stdin, and the file that lands must be named
// for the right pair and be byte-identical to the fixture it came from.
//
// A capture that does not reproduce a fixture is a capture whose output a
// reviewer would have to hand-edit before committing, which is the work
// this harness exists to remove.
func TestWriteReproducesCommittedFixtures(t *testing.T) {
	dir := t.TempDir()
	files := committedInputFixtures(t)
	for _, f := range files {
		t.Run(f.host+"/"+string(f.event), func(t *testing.T) {
			rec, err := capture.Write(f.host, dir, f.raw)
			if err != nil {
				t.Fatalf("Write: %v", err)
			}
			if rec.Event != f.event {
				t.Errorf("recorded event %q; the envelope came from the %q fixture", rec.Event, f.event)
			}
			wantName := string(f.event) + ".input.json"
			if got := filepath.Base(rec.Path); got != wantName {
				t.Errorf("capture file is %q, want %q", got, wantName)
			}
			if filepath.Base(filepath.Dir(rec.Path)) != f.host {
				t.Errorf("capture landed in %q, want a %q directory", rec.Path, f.host)
			}
			onDisk, err := os.ReadFile(rec.Path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if string(onDisk) != string(f.raw) {
				t.Errorf("capture differs from the committed fixture it came from\n--- fixture\n%s\n--- capture\n%s", f.raw, onDisk)
			}
		})
	}
}

// TestWriteTakesTheEventFromTheEnvelope: a capture's file name decides
// which pair it can become a fixture for, so it must come from the
// envelope's own discriminator. Here a gemini envelope naming host event
// AfterTool is recorded, and must land under the canonical name
// PostToolUse that gemini's capability map assigns it — not under the
// host-side name, and not under whatever a caller might have assumed.
func TestWriteTakesTheEventFromTheEnvelope(t *testing.T) {
	dir := t.TempDir()
	raw, err := fs.ReadFile(axon.Spec(), path.Join("fixtures", "hosts", "gemini", "PostToolUse.input.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"AfterTool"`) {
		t.Fatal("this test relies on the gemini PostToolUse fixture carrying host event AfterTool")
	}
	rec, err := capture.Write(axon.HostGemini, dir, raw)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if rec.Event != axon.EventPostToolUse {
		t.Errorf("recorded event %q, want the canonical %q", rec.Event, axon.EventPostToolUse)
	}
}

// TestWriteRefusesSpecDirectories is the guard on the rule the whole
// harness rests on: the tool never writes a fixture. A --dir aimed at the
// committed spec tree, or at the generated Go mirror of it, is refused.
func TestWriteRefusesSpecDirectories(t *testing.T) {
	raw := []byte(`{"hook_event_name":"Stop","session_id":"s","cwd":"/x"}`)
	root := t.TempDir()
	for _, sub := range []string{
		// The spec ROOT itself, which naming only its known subtrees
		// left wide open: a bare `--dir <repo>/spec` recorded straight
		// into the committed tree with no refusal at all.
		"spec",
		"go/spec",
		// Its subtrees, and any directory added under it later.
		"spec/fixtures",
		"spec/fixtures/hosts/claude",
		"spec/hosts",
		"spec/hosts/claude/hooks",
		"spec/some/dir/added/later",
		"go/spec/fixtures/hosts/claude",
		"deeply/nested/repo/spec",
	} {
		dir := filepath.Join(root, sub)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if _, err := capture.Write(axon.HostClaude, dir, raw); !errors.Is(err, capture.ErrCaptureIntoSpec) {
			t.Errorf("Write into %s: err = %v; want ErrCaptureIntoSpec", sub, err)
		}
	}
	// A directory that merely looks similar is a different path segment
	// and must not be refused, or the guard would start rejecting
	// ordinary scratch directories.
	for _, sub := range []string{"spec-notes", "specimens", "myspec/fixtures", "gospec", "spec2/hosts"} {
		ok := filepath.Join(root, sub)
		if err := os.MkdirAll(ok, 0o750); err != nil {
			t.Fatal(err)
		}
		if _, err := capture.Write(axon.HostClaude, ok, raw); err != nil {
			t.Errorf("Write into %s was refused: %v", sub, err)
		}
	}
}

// TestWriteReportsReplacement: exercising the same event twice must be
// visibly a no-op when the shape is stable and visibly a change when it is
// not, because a changed re-capture is the signal that the first recording
// missed a variant.
func TestWriteReportsReplacement(t *testing.T) {
	dir := t.TempDir()
	first := []byte(`{"hook_event_name":"Stop","session_id":"a","cwd":"/x","reason":"end_turn"}`)
	rec, err := capture.Write(axon.HostClaude, dir, first)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Replaced || rec.Changed {
		t.Errorf("first capture reports Replaced=%v Changed=%v, want false/false", rec.Replaced, rec.Changed)
	}
	// Same shape, different volatile values: normalization erases the
	// difference, so this must read as unchanged.
	second := []byte(`{"hook_event_name":"Stop","session_id":"b","cwd":"/elsewhere","reason":"end_turn"}`)
	rec, err = capture.Write(axon.HostClaude, dir, second)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Replaced || rec.Changed {
		t.Errorf("re-capture of the same shape reports Replaced=%v Changed=%v, want true/false", rec.Replaced, rec.Changed)
	}
	// A genuinely different shape must read as changed.
	third := []byte(`{"hook_event_name":"Stop","session_id":"c","cwd":"/x","reason":"interrupted","stop_hook_active":true}`)
	rec, err = capture.Write(axon.HostClaude, dir, third)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Replaced || !rec.Changed {
		t.Errorf("re-capture of a different shape reports Replaced=%v Changed=%v, want true/true", rec.Replaced, rec.Changed)
	}
}

// TestWriteRejectsUndecodableEnvelopes: a payload the host's codec cannot
// place is not written. A file on disk named for a pair is a claim about
// that pair, and an unplaceable envelope supports no such claim.
func TestWriteRejectsUndecodableEnvelopes(t *testing.T) {
	dir := t.TempDir()
	for name, raw := range map[string]string{
		"no discriminator": `{"session_id":"s"}`,
		"unknown event":    `{"hook_event_name":"NotAnEvent","session_id":"s"}`,
		"not json":         `not json at all`,
	} {
		if _, err := capture.Write(axon.HostClaude, dir, []byte(raw)); err == nil {
			t.Errorf("Write(%s) returned nil error", name)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("rejected envelopes still created %d entries under the capture directory", len(entries))
	}
}

// TestSnippetSubscribesToHostSideNames: an operator pastes the snippet
// into a host settings file, so its keys must be the names that host uses
// on the wire, not axon's canonical ones. gemini is the case that
// distinguishes them.
func TestSnippetSubscribesToHostSideNames(t *testing.T) {
	s, err := capture.SnippetFor(axon.HostGemini, handlerArgv(axon.HostGemini), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.Body, `"BeforeTool"`) {
		t.Errorf("gemini snippet does not subscribe to BeforeTool:\n%s", s.Body)
	}
	if strings.Contains(s.Body, `"PreToolUse"`) {
		t.Errorf("gemini snippet uses the canonical name PreToolUse; gemini's wire name is BeforeTool:\n%s", s.Body)
	}
}

// TestSnippetConfigPathsComeFromHostYAML: the paths an operator is told to
// edit must be the host's own, from spec/hosts/<host>/host.yaml, never a
// remembered default.
func TestSnippetConfigPathsComeFromHostYAML(t *testing.T) {
	for _, host := range []string{axon.HostClaude, axon.HostCodex, axon.HostGemini, axon.HostOpencode} {
		h, ok := axon.Get(host)
		if !ok {
			t.Fatalf("%s missing from the spec", host)
		}
		s, err := capture.SnippetFor(host, handlerArgv(host), true)
		if err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		if len(s.ConfigPaths) == 0 {
			t.Errorf("%s: snippet names no config path", host)
		}
		if strings.Join(s.ConfigPaths, ",") != strings.Join(h.HookConfigPaths, ",") {
			t.Errorf("%s: snippet paths %v, host.yaml hook_config_paths %v", host, s.ConfigPaths, h.HookConfigPaths)
		}
	}
}

// TestSnippetDefaultsToTheUncapturedEvents: the point of installing a
// handler is to record what is missing, so the default subscription is the
// missing set — and it must be smaller than the full one, or the default
// is doing nothing.
func TestSnippetDefaultsToTheUncapturedEvents(t *testing.T) {
	missing, err := capture.HandlerEvents(axon.HostClaude, false)
	if err != nil {
		t.Fatal(err)
	}
	all, err := capture.HandlerEvents(axon.HostClaude, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) >= len(all) {
		t.Errorf("default subscription (%d events) is not narrower than --all (%d)", len(missing), len(all))
	}
	if len(missing) == 0 {
		t.Error("default subscription is empty; there are uncaptured claude events")
	}
	for _, ev := range missing {
		if ev == "PreToolUse" {
			t.Error("default subscription includes PreToolUse, which already has a committed fixture")
		}
	}
}

// TestSnippetRefusesHostsWithoutHooks: a host with no hook surface has no
// snippet, and saying so beats printing configuration that cannot work.
func TestSnippetRefusesHostsWithoutHooks(t *testing.T) {
	var noHooks string
	for _, h := range axon.Hosts() {
		if !h.Hooks {
			noHooks = h.Name
			break
		}
	}
	if noHooks == "" {
		t.Skip("every host in the spec has hooks: true")
	}
	if _, err := capture.SnippetFor(noHooks, handlerArgv(noHooks), true); err == nil {
		t.Errorf("SnippetFor(%q) returned a snippet for a host with hooks: false", noHooks)
	}
}

// TestOpencodeSnippetNeverThrows: an OpenCode hook blocks by throwing
// inside the plugin callback, so a capture plugin that can throw is a
// capture plugin that can stop a tool call.
func TestOpencodeSnippetNeverThrows(t *testing.T) {
	s, err := capture.SnippetFor(axon.HostOpencode, handlerArgv(axon.HostOpencode), true)
	if err != nil {
		t.Fatal(err)
	}
	// Only executable lines matter; the header comment explains why the
	// word must not appear in one.
	for _, line := range strings.Split(s.Body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		if strings.Contains(line, "throw") {
			t.Errorf("the opencode plugin can throw, which is how an OpenCode hook blocks: %q", line)
		}
	}
	if !strings.Contains(s.Body, "try {") || !strings.Contains(s.Body, "catch") {
		t.Errorf("the opencode plugin does not wrap its work in try/catch:\n%s", s.Body)
	}
	if s.Warning == "" {
		t.Error("the opencode snippet carries no warning about in-process callbacks")
	}
	if !strings.Contains(s.Body, `"tool.execute.before"`) {
		t.Errorf("the opencode plugin does not subscribe to the dotted host event names:\n%s", s.Body)
	}
}

// TestSnippetHandlerCommandCarriesTheHost: `capture handle` needs the host
// name, so a snippet whose command line omits it would install a handler
// that fails on every invocation.
func TestSnippetHandlerCommandCarriesTheHost(t *testing.T) {
	for _, host := range []string{axon.HostClaude, axon.HostCodex, axon.HostGemini, axon.HostOpencode} {
		argv := []string{"/usr/local/bin/axon", "capture", "handle", host, "--dir", "/tmp/caps"}
		s, err := capture.SnippetFor(host, argv, true)
		if err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		// Every snippet must carry the binary, the subcommand words, the
		// host and the capture directory, however its own syntax renders
		// them (settings JSON keeps the line intact; the opencode plugin
		// splits it into spawn's command and argv).
		for _, word := range []string{"/usr/local/bin/axon", "capture", "handle", host, "--dir", "/tmp/caps"} {
			if !strings.Contains(s.Body, word) {
				t.Errorf("%s: snippet body is missing %q from the handler command:\n%s", host, word, s.Body)
			}
		}
	}
}

// handlerArgv is the argv shape the CLI passes to SnippetFor.
func handlerArgv(host string) []string {
	return []string{"axon", "capture", "handle", host}
}

// TestSnippetSurvivesSpacesInPaths: a binary under "Application Support"
// or a capture directory with a space in it must still produce a working
// handler command. The argv was once joined into a string and re-split on
// whitespace, which shredded both — and the opencode plugin swallows spawn
// errors, so the operator would have got a silently non-recording plugin
// with no diagnostic at all.
func TestSnippetSurvivesSpacesInPaths(t *testing.T) {
	argv := []string{"/Users/a b/Library/Application Support/axon", "capture", "handle", "", "--dir", "/tmp/my captures"}
	for _, host := range []string{axon.HostClaude, axon.HostCodex, axon.HostGemini, axon.HostOpencode} {
		a := append([]string(nil), argv...)
		a[3] = host
		s, err := capture.SnippetFor(host, a, true)
		if err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		// The full paths must appear intact somewhere in the body.
		for _, want := range []string{"/Users/a b/Library/Application Support/axon", "/tmp/my captures"} {
			if !strings.Contains(s.Body, want) {
				t.Errorf("%s: %q was split apart in the snippet:\n%s", host, want, s.Body)
			}
		}
	}
}

// TestSnippetQuotesShellMetacharacters: the three settings-file hosts hold
// the handler as a single command STRING that the host splits, so a
// directory carrying a metacharacter must not be able to start a second
// command.
func TestSnippetQuotesShellMetacharacters(t *testing.T) {
	argv := []string{"/usr/local/bin/axon", "capture", "handle", axon.HostClaude, "--dir", "/tmp/x; touch /tmp/pwned"}
	s, err := capture.SnippetFor(axon.HostClaude, argv, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s.Body, "--dir /tmp/x; touch") {
		t.Errorf("the directory was interpolated unquoted, so it can start a second command:\n%s", s.Body)
	}
	if !strings.Contains(s.Body, "'/tmp/x; touch /tmp/pwned'") {
		t.Errorf("the directory was not single-quoted:\n%s", s.Body)
	}
}

// TestEveryRegisteredHostHasASnippet: a host with a codec is a host whose
// envelopes can be captured, so it must also be one an operator can be
// told how to configure.
func TestEveryRegisteredHostHasASnippet(t *testing.T) {
	for _, host := range hooks.Registered() {
		if _, err := capture.SnippetFor(host, handlerArgv(host), true); err != nil {
			t.Errorf("%s has a codec but no install snippet: %v", host, err)
		}
	}
}
