package capture_test

import (
	"io/fs"
	"path"
	"strings"
	"testing"

	"hop.top/axon"
	"hop.top/axon/capture"
	"hop.top/axon/hooks"
	_ "hop.top/axon/hooks/hosts"
)

// TestNormalizeRoundTripsCommittedFixtures is the proof that this
// package's conventions ARE the tree's conventions rather than a parallel
// set of guesses. Every committed input fixture is fed back through
// Normalize; a byte-identical result means indentation, key order,
// placeholder values, nested-object rendering and the trailing newline all
// match what is already on disk.
//
// A failure here is a bug in this package, never a reason to edit a
// fixture: the fixtures are the recorded evidence, and normalization
// exists to produce more files that look like them.
func TestNormalizeRoundTripsCommittedFixtures(t *testing.T) {
	files := committedInputFixtures(t)
	if len(files) == 0 {
		t.Fatal("no committed input fixtures found; the round-trip proof is vacuous")
	}
	for _, f := range files {
		t.Run(f.host+"/"+string(f.event), func(t *testing.T) {
			got, err := capture.Normalize(f.host, f.event, f.raw)
			if err != nil {
				t.Fatalf("Normalize: %v", err)
			}
			if string(got) != string(f.raw) {
				t.Errorf("normalizing a committed fixture is not a no-op\n--- want (on disk)\n%s\n--- got\n%s", f.raw, got)
			}
		})
	}
}

// TestNormalizeIsIdempotent: normalizing an already-normalized capture
// must change nothing, so an operator can re-run the tool over a
// capture directory without churning files.
func TestNormalizeIsIdempotent(t *testing.T) {
	raw := []byte(`{"session_id":"live-abc","cwd":"/Users/someone/work","hook_event_name":"SessionEnd","reason":"exit"}`)
	once, err := capture.Normalize(axon.HostClaude, axon.EventSessionEnd, raw)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	twice, err := capture.Normalize(axon.HostClaude, axon.EventSessionEnd, once)
	if err != nil {
		t.Fatalf("Normalize (second pass): %v", err)
	}
	if string(once) != string(twice) {
		t.Errorf("not idempotent\nfirst:\n%s\nsecond:\n%s", once, twice)
	}
}

// TestNormalizeReplacesVolatileValues pins the substitutions on an
// envelope shaped like one a live host would send: a real session uuid, a
// real home-directory cwd, a real transcript path.
func TestNormalizeReplacesVolatileValues(t *testing.T) {
	raw := []byte(`{
	  "hook_event_name": "SessionEnd",
	  "session_id": "7f3a1c92-55de-4b21-9e0c-2d8a44b17c05",
	  "cwd": "/Users/someone/src/project",
	  "transcript_path": "/Users/someone/.claude/projects/x/7f3a.jsonl",
	  "reason": "exit"
	}`)
	got, err := capture.Normalize(axon.HostClaude, axon.EventSessionEnd, raw)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	for _, leak := range []string{"7f3a1c92", "/Users/someone", ".claude/projects"} {
		if strings.Contains(string(got), leak) {
			t.Errorf("normalized capture still carries %q:\n%s", leak, got)
		}
	}
	for _, want := range []string{capture.SessionID, capture.Cwd, capture.TranscriptPath} {
		if !strings.Contains(string(got), want) {
			t.Errorf("normalized capture is missing placeholder %q:\n%s", want, got)
		}
	}
}

// TestNormalizePreservesUnknownKeys: a captured envelope carries whatever
// the host actually sent, including keys no schema names yet — that being
// the point of capturing it. Normalization orders and indents; it never
// drops.
func TestNormalizePreservesUnknownKeys(t *testing.T) {
	raw := []byte(`{"hook_event_name":"PreCompact","session_id":"s","cwd":"/x","trigger":"auto","custom_instructions":""}`)
	got, err := capture.Normalize(axon.HostClaude, axon.EventPreCompact, raw)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	for _, key := range []string{"trigger", "custom_instructions"} {
		if !strings.Contains(string(got), `"`+key+`"`) {
			t.Errorf("normalization dropped unrecorded key %q:\n%s", key, got)
		}
	}
}

// TestNormalizeRejectsNonObject: a hook envelope is a JSON object. An
// array, a scalar or a truncated read is a recording failure, and saying
// so beats writing a file that looks like a capture.
func TestNormalizeRejectsNonObject(t *testing.T) {
	for _, raw := range []string{`[]`, `"text"`, `{"unterminated":`, ``} {
		if _, err := capture.Normalize(axon.HostClaude, axon.EventStop, []byte(raw)); err == nil {
			t.Errorf("Normalize(%q) returned nil error; want a decode failure", raw)
		}
	}
}

// TestNormalizeUnknownHost: the host name gates the key order and the
// config snippet, so an unknown one is an error rather than a capture
// ordered by a silent default.
func TestNormalizeUnknownHost(t *testing.T) {
	if _, err := capture.Normalize("not-a-host", axon.EventStop, []byte(`{}`)); err == nil {
		t.Error("Normalize with an unknown host returned nil error")
	}
}

// TestLeadOrderMatchesCommittedFixtures proves the per-host lead order is
// the tree's order and not an invention, independently of the schema path:
// it re-normalizes every committed fixture with its schema deliberately
// out of reach (an event name no schema file exists for), which forces the
// leadOrder fallback, and requires the same key sequence.
//
// Without this, leadOrder could be wrong for every host and
// TestNormalizeRoundTripsCommittedFixtures would still pass, since that
// test always finds a schema — and leadOrder is exactly the path a real
// capture of a deferred pair takes.
func TestLeadOrderMatchesCommittedFixtures(t *testing.T) {
	for _, f := range committedInputFixtures(t) {
		t.Run(f.host+"/"+string(f.event), func(t *testing.T) {
			// "NoSuchEventForSchemaLookup" has no input schema for any
			// host, so keyOrder falls through to leadOrder[host].
			got, err := capture.Normalize(f.host, axon.Event("NoSuchEventForSchemaLookup"), f.raw)
			if err != nil {
				t.Fatalf("Normalize: %v", err)
			}
			if want, have := keySequence(t, f.raw), keySequence(t, got); !equalStrings(want, have) {
				t.Errorf("leadOrder[%s] orders keys %v; the committed fixture orders them %v", f.host, have, want)
			}
		})
	}
}

type fixture struct {
	host  string
	event axon.Event
	raw   []byte
}

// committedInputFixtures reads every <Event>.input.json under
// spec/fixtures/hosts/ for a host with a registered codec.
func committedInputFixtures(t *testing.T) []fixture {
	t.Helper()
	var out []fixture
	for _, host := range hooks.Registered() {
		dir := path.Join("fixtures", "hosts", host)
		entries, err := fs.ReadDir(axon.Spec(), dir)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		for _, e := range entries {
			parts := strings.Split(e.Name(), ".")
			if len(parts) != 3 || parts[1] != "input" {
				continue
			}
			raw, err := fs.ReadFile(axon.Spec(), path.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", e.Name(), err)
			}
			out = append(out, fixture{host: host, event: axon.Event(parts[0]), raw: raw})
		}
	}
	return out
}

// keySequence returns the top-level keys of a normalized document in the
// order the bytes present them. It reads the rendered text rather than
// decoding into a map, because a map loses exactly the property under
// test.
func keySequence(t *testing.T, doc []byte) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(string(doc), "\n") {
		if !strings.HasPrefix(line, capture.Indent+`"`) {
			continue
		}
		rest := strings.TrimPrefix(line, capture.Indent+`"`)
		if i := strings.Index(rest, `"`); i > 0 {
			out = append(out, rest[:i])
		}
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
