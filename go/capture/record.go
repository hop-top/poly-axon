package capture

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hop.top/axon"
	"hop.top/axon/hooks"
)

// Record is the result of recording one envelope.
type Record struct {
	// Host and Event identify the pair, as decoded from the envelope
	// itself rather than as claimed by the caller.
	Host  string
	Event axon.Event
	// Path is the file the normalized envelope was written to.
	Path string
	// Normalized is the bytes written.
	Normalized []byte
	// Replaced reports whether a capture for this pair already existed and
	// was overwritten with identical or differing content.
	Replaced bool
	// Changed reports whether the content differs from what was there
	// before. False on a first capture and on a byte-identical re-capture,
	// which is what an operator wants to see when they exercise the same
	// event twice.
	Changed bool
}

// ErrCaptureIntoSpec guards the one thing this package must never do.
var ErrCaptureIntoSpec = errors.New("capture: refusing to write inside spec/")

// Write decodes raw with the host's codec to learn which event it is,
// normalizes it, and writes it under dir as <dir>/<host>/<Event>.input.json.
//
// The event comes from the envelope, never from an argument: the whole
// point of a capture is to record what the host actually sent, and letting
// a caller assert the event would let a mislabelled file become a fixture
// for the wrong pair. A payload whose discriminator names an event the
// host's capability file does not carry is an error, not a file.
//
// dir is the operator's own working directory. Writing into spec/ is
// refused outright: a captured envelope is evidence pending review, and
// the review is a person reading a diff, not this function's opinion.
func Write(host, dir string, raw []byte) (Record, error) {
	if dir == "" {
		return Record{}, errors.New("capture: no capture directory given")
	}
	if err := refuseSpecDir(dir); err != nil {
		return Record{}, err
	}
	codec, ok := hooks.For(host)
	if !ok {
		return Record{}, fmt.Errorf("capture: no codec registered for host %q", host)
	}
	in, err := codec.DecodeInput(raw)
	if err != nil {
		return Record{}, fmt.Errorf("capture: %s: %w", host, err)
	}
	normalized, err := Normalize(host, in.Event, raw)
	if err != nil {
		return Record{}, err
	}
	path := CapturePath(dir, host, in.Event)
	rec := Record{Host: host, Event: in.Event, Path: path, Normalized: normalized}
	if prev, err := os.ReadFile(path); err == nil { //nolint:gosec // path is built from the operator's own -dir plus a validated event name
		rec.Replaced = true
		rec.Changed = string(prev) != string(normalized)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return Record{}, fmt.Errorf("capture: %w", err)
	}
	// 0644 matches the mode every committed fixture carries, so promoting a
	// capture is a plain copy and not a copy plus a chmod. A capture holds
	// no secret by construction: normalization has already replaced the
	// session id and every absolute path with a placeholder.
	if err := os.WriteFile(path, normalized, 0o644); err != nil { //nolint:gosec // deliberate: matches the mode of the committed fixtures a capture is promoted into
		return Record{}, fmt.Errorf("capture: %w", err)
	}
	return rec, nil
}

// refuseSpecDir rejects a capture directory that is, or sits under, a
// directory named "spec". The tool must never write a fixture on the
// operator's behalf; a -dir pointing into spec/ would do exactly that by
// the back door, and one pointing into the generated go/spec/ mirror would
// additionally desync `make generate-check`.
//
// The test is on the path SEGMENT, not on a set of known subtrees. Naming
// spec/fixtures, spec/hosts and go/spec individually left the tree's own
// root — a bare `--dir <repo>/spec` — wide open, along with every
// directory anyone adds under it later. Matching the segment closes both.
// A sibling like "spec-notes" or "specimens" is a different segment and is
// not refused.
//
// The cost is a false positive for an operator whose unrelated scratch
// directory happens to live under some other "spec" folder. That is the
// right way round: the failure is a visible refusal naming --dir, and the
// alternative failure is an unreviewed envelope silently landing in the
// committed tree.
func refuseSpecDir(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("capture: %w", err)
	}
	for _, segment := range strings.Split(filepath.ToSlash(filepath.Clean(abs)), "/") {
		if segment == "spec" {
			return fmt.Errorf("%w: %s is inside a spec tree; capture to a scratch directory and promote a reviewed file by hand", ErrCaptureIntoSpec, dir)
		}
	}
	return nil
}
