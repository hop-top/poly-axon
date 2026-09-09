package capture_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"hop.top/axon"
	"hop.top/axon/capture"
	_ "hop.top/axon/hooks/hosts"
)

// TestCoverageNamesEveryRefusedPair ties the report to the CLI behaviour
// it exists to explain: a pair Coverage marks Refused is exactly a pair
// `axon fixture <host> <event>` refuses, because both conditions are the
// same one — no spec/hosts/<host>/hooks/<Event>.input.schema.json
// (go/cmd/fixture.go's fs.Stat gate).
//
// The count is asserted so the report cannot quietly stop naming pairs.
// It is not a frozen number: landing a schema lowers it, which is the
// point, and the failure message says so.
func TestCoverageNamesEveryRefusedPair(t *testing.T) {
	pairs, err := capture.Coverage("")
	if err != nil {
		t.Fatal(err)
	}
	var refused []string
	for _, p := range pairs {
		if !p.Refused() {
			continue
		}
		refused = append(refused, p.Host+" "+string(p.Event))
		// Cross-check against the filesystem directly rather than
		// trusting the same field the report was built from.
		if _, err := fs.Stat(axon.Spec(), p.SchemaPath); err == nil {
			t.Errorf("%s/%s is reported refused but spec/%s exists", p.Host, p.Event, p.SchemaPath)
		}
	}
	sort.Strings(refused)
	const want = 30
	if len(refused) != want {
		t.Errorf("Coverage names %d refused pairs, want %d.\n"+
			"If a schema just landed this number should DROP and the expectation moves with it; "+
			"if it rose, a schema was deleted.\nPairs:\n  %s",
			len(refused), want, strings.Join(refused, "\n  "))
	}
}

// TestCoverageCoversEveryRegisteredHostEvent: the report must include
// every native and close row of every registered host, so "not listed" can
// never be mistaken for "nothing to do".
func TestCoverageCoversEveryRegisteredHostEvent(t *testing.T) {
	pairs, err := capture.Coverage("")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, p := range pairs {
		seen[p.Host+"/"+string(p.Event)] = true
	}
	for _, host := range registeredHosts(t) {
		for _, ev := range hostEventsFor(t, host) {
			if !seen[host+"/"+string(ev)] {
				t.Errorf("%s/%s is native or close in capabilities.yaml but absent from the coverage report", host, ev)
			}
		}
	}
}

// TestCoverageReflectsFixturePresence: a pair with a committed fixture is
// "fixture", one without is "missing", and dropping a capture into the
// directory moves it to "captured". The statuses are read off the
// filesystem, so this is what stops the report drifting from the tree.
func TestCoverageReflectsFixturePresence(t *testing.T) {
	dir := t.TempDir()
	before, err := capture.Coverage(dir)
	if err != nil {
		t.Fatal(err)
	}
	// claude PreToolUse has a committed fixture; claude PreCompact does not.
	if got := statusFor(t, before, axon.HostClaude, axon.EventPreToolUse); got != capture.StatusFixture {
		t.Errorf("claude/PreToolUse status = %q, want %q", got, capture.StatusFixture)
	}
	if got := statusFor(t, before, axon.HostClaude, axon.EventPreCompact); got != capture.StatusMissing {
		t.Errorf("claude/PreCompact status = %q, want %q", got, capture.StatusMissing)
	}

	path := capture.CapturePath(dir, axon.HostClaude, axon.EventPreCompact)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := capture.Coverage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := statusFor(t, after, axon.HostClaude, axon.EventPreCompact); got != capture.StatusCaptured {
		t.Errorf("after writing a capture, claude/PreCompact status = %q, want %q", got, capture.StatusCaptured)
	}
}

// TestCoverageCarriesTheGeminiUserPromptSubmitCaveat: gemini's
// UserPromptSubmit is a close row onto host event BeforeModel, and its
// capability note is the whole reason a capture of it is an open question
// rather than a formality. The report must carry that note to the reader,
// not drop it.
func TestCoverageCarriesTheGeminiUserPromptSubmitCaveat(t *testing.T) {
	pairs, err := capture.Coverage("")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pairs {
		if p.Host != axon.HostGemini || p.Event != axon.EventUserPromptSubmit {
			continue
		}
		if p.HostEvent != "BeforeModel" {
			t.Errorf("host event = %q, want BeforeModel", p.HostEvent)
		}
		if p.Note == "" {
			t.Error("the capability row's note is empty in the report; the caveat is the point of this row")
		}
		if !strings.Contains(capture.NextStep(p), p.Note) {
			t.Errorf("NextStep = %q, want it to carry the note %q", capture.NextStep(p), p.Note)
		}
		return
	}
	t.Fatal("gemini/UserPromptSubmit is absent from the coverage report")
}

// TestSummarizeCounts: the totals printed under the table must add up to
// the number of rows above it.
func TestSummarizeCounts(t *testing.T) {
	pairs, err := capture.Coverage("")
	if err != nil {
		t.Fatal(err)
	}
	f, c, m := capture.Summarize(pairs)
	if f+c+m != len(pairs) {
		t.Errorf("summary %d+%d+%d does not add up to %d rows", f, c, m, len(pairs))
	}
	if f == 0 || m == 0 {
		t.Errorf("summary has no fixture rows (%d) or no missing rows (%d); one of the two counts is broken", f, m)
	}
}

func statusFor(t *testing.T, pairs []capture.Pair, host string, ev axon.Event) capture.Status {
	t.Helper()
	for _, p := range pairs {
		if p.Host == host && p.Event == ev {
			return p.Status
		}
	}
	t.Fatalf("%s/%s absent from the coverage report", host, ev)
	return ""
}

func registeredHosts(t *testing.T) []string {
	t.Helper()
	pairs, err := capture.Coverage("")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range pairs {
		if !seen[p.Host] {
			seen[p.Host] = true
			out = append(out, p.Host)
		}
	}
	if len(out) == 0 {
		t.Fatal("no hosts in the coverage report")
	}
	return out
}

func hostEventsFor(t *testing.T, host string) []axon.Event {
	t.Helper()
	var caps struct {
		Native []struct {
			Event axon.Event `yaml:"event"`
		} `yaml:"native"`
		Close []struct {
			Event axon.Event `yaml:"event"`
		} `yaml:"close"`
	}
	if err := axon.LoadSpecYAML(fmt.Sprintf("hosts/%s/capabilities.yaml", host), "capabilities.schema.json", &caps); err != nil {
		t.Fatalf("%s: %v", host, err)
	}
	var out []axon.Event
	for _, p := range caps.Native {
		out = append(out, p.Event)
	}
	for _, p := range caps.Close {
		out = append(out, p.Event)
	}
	return out
}
