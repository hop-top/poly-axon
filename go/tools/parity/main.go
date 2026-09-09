// Command go (the axon parity emitter) answers one parity-harness case
// against the Go reference implementation. See tools/parity/README.md for
// the invocation contract this program implements.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"

	"hop.top/axon"
	"hop.top/axon/hooks"
	_ "hop.top/axon/hooks/hosts"
)

// caseFile mirrors tools/parity/cases.json's shape.
type caseFile struct {
	ID   string         `json:"id"`
	Call string         `json:"call"`
	Args map[string]any `json:"args"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go-emitter <case-id>")
		os.Exit(2)
	}
	caseID := os.Args[1]

	cases, err := loadCases()
	if err != nil {
		fmt.Fprintln(os.Stderr, "go-emitter: load cases.json:", err)
		os.Exit(2)
	}
	c, ok := cases[caseID]
	if !ok {
		fmt.Fprintf(os.Stderr, "go-emitter: unknown case id %q\n", caseID)
		os.Exit(2)
	}

	result, err := dispatch(c)
	if err != nil {
		fmt.Fprintln(os.Stderr, "go-emitter:", err)
		os.Exit(1)
	}
	if err := writeCanonicalJSON(result); err != nil {
		fmt.Fprintln(os.Stderr, "go-emitter: encode result:", err)
		os.Exit(1)
	}
}

func loadCases() (map[string]caseFile, error) {
	// This path is relative to the working directory, not to this
	// program's source: parity.py always *runs* an emitter with
	// cwd=repo root (only the Go build runs from go/, so that `go
	// build` can find go.mod), and the frozen emitter contract has
	// every language read tools/parity/cases.json by that path.
	raw, err := os.ReadFile("tools/parity/cases.json")
	if err != nil {
		return nil, err
	}
	var list []caseFile
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make(map[string]caseFile, len(list))
	for _, c := range list {
		out[c.ID] = c
	}
	return out, nil
}

func dispatch(c caseFile) (any, error) {
	switch c.Call {
	case "resolve":
		return callResolve(c.Args)
	case "hosts":
		return callHosts()
	case "hooked_hosts":
		return callHookedHosts()
	case "native_events":
		return callNativeEvents()
	case "capability_level":
		return callCapabilityLevel(c.Args)
	case "host_event":
		return callHostEvent(c.Args)
	case "encode_input":
		return callEncodeInput(c.Args)
	case "decode_decision":
		return callDecodeDecision(c.Args)
	default:
		return map[string]any{"unsupported": true}, nil
	}
}

func argString(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}

func argInt(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

func callResolve(args map[string]any) (any, error) {
	h, found := axon.Resolve(argString(args, "name"))
	return map[string]any{"found": found, "name": h.Name}, nil
}

func callHosts() (any, error) {
	var names []string
	for _, h := range axon.Hosts() {
		names = append(names, h.Name)
	}
	sort.Strings(names)
	return names, nil
}

func callHookedHosts() (any, error) {
	var names []string
	for _, h := range axon.HookedHosts() {
		names = append(names, h.Name)
	}
	sort.Strings(names)
	return names, nil
}

func callNativeEvents() (any, error) {
	var names []string
	for _, e := range axon.NativeEvents() {
		names = append(names, string(e))
	}
	sort.Strings(names)
	return names, nil
}

// capsFor loads a host's capabilities via the public hooks.LoadCapabilities,
// which returns hooks.ErrUnknownHost for a name axon does not recognize.
func capsFor(host string) (hooks.Capabilities, error) {
	return hooks.LoadCapabilities(host)
}

func callCapabilityLevel(args map[string]any) (any, error) {
	caps, err := capsFor(argString(args, "host"))
	if err != nil {
		return sentinelResult(err)
	}
	level, classified := caps.Level(axon.Event(argString(args, "event")))
	return map[string]any{"level": string(level), "classified": classified}, nil
}

func callHostEvent(args map[string]any) (any, error) {
	caps, err := capsFor(argString(args, "host"))
	if err != nil {
		return sentinelResult(err)
	}
	hostEvent, found := caps.HostEvent(axon.Event(argString(args, "event")))
	return map[string]any{"host_event": hostEvent, "found": found}, nil
}

func readFixture(host, fixture string) ([]byte, error) {
	return fs.ReadFile(axon.Spec(), path.Join("fixtures", "hosts", host, fixture))
}

// callEncodeInput answers `encode_input`. When the named fixture exists on
// disk, it decodes the real file and re-encodes it (the round-trip §6 of
// docs/spec-contract-notes.md guarantees for input fixtures). A fixture
// name is also allowed to not exist on disk — this is how the harness
// probes the ErrUnsupportedEvent path without inventing a second args
// shape: the <Event> is parsed from the fixture name and a minimal
// synthetic Input is built directly, the same way
// TestEncodeInputRequiresNativeOrClose does.
func callEncodeInput(args map[string]any) (any, error) {
	host := argString(args, "host")
	fixture := argString(args, "fixture")

	c, ok := hooks.For(host)
	if !ok {
		return sentinelResult(fmt.Errorf("%w: %q", hooks.ErrUnknownHost, host))
	}

	var in hooks.Input
	raw, err := readFixture(host, fixture)
	switch {
	case err == nil:
		in, err = c.DecodeInput(raw)
		if err != nil {
			return sentinelResult(err)
		}
	case errors.Is(err, fs.ErrNotExist):
		in = hooks.Input{Event: eventFromFixtureName(fixture), SessionID: "s", Cwd: "/tmp"}
	default:
		return sentinelResult(fmt.Errorf("%w: %v", hooks.ErrSchema, err))
	}

	again, err := c.EncodeInput(in)
	if err != nil {
		return sentinelResult(err)
	}
	var value any
	if err := json.Unmarshal(again, &value); err != nil {
		return sentinelResult(fmt.Errorf("%w: %v", hooks.ErrSchema, err))
	}
	return value, nil
}

// callDecodeDecision answers `decode_decision`. When args carries a "raw"
// string, that literal string is decoded instead of a fixture file — this
// is how the three fallback cases from docs/spec-contract-notes.md §6
// (empty stdout, "{}", unrecognized JSON, all with a block exit) are
// expressed without inventing fixture files nobody captured from a host.
func callDecodeDecision(args map[string]any) (any, error) {
	host := argString(args, "host")
	fixture := argString(args, "fixture")
	exit := argInt(args, "exit")

	c, ok := hooks.For(host)
	if !ok {
		return sentinelResult(fmt.Errorf("%w: %q", hooks.ErrUnknownHost, host))
	}

	var raw []byte
	if rawArg, ok := args["raw"]; ok {
		s, _ := rawArg.(string)
		raw = []byte(s)
	} else {
		var err error
		raw, err = readFixture(host, fixture)
		if err != nil {
			return sentinelResult(fmt.Errorf("%w: %v", hooks.ErrSchema, err))
		}
	}
	event := eventFromFixtureName(fixture)
	d, err := c.DecodeDecision(raw, exit)
	if err != nil {
		return sentinelResult(err)
	}
	// The fixture's file name carries the event; set it as the reference
	// conformance test does, before reporting the decoded result.
	d.Event = event
	return map[string]any{
		"action":   string(d.Action),
		"event":    string(d.Event),
		"message":  d.Message,
		"metadata": metadataOrEmpty(d.Metadata),
	}, nil
}

func metadataOrEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// eventFromFixtureName splits "<Event>.<kind>.json" and returns <Event>,
// matching hooks/conformance_test.go's parsing.
func eventFromFixtureName(fixture string) axon.Event {
	base := fixture
	for i := 0; i < len(base); i++ {
		if base[i] == '.' {
			return axon.Event(base[:i])
		}
	}
	return axon.Event(base)
}

// sentinelResult maps a Go error onto one of the four contract sentinel
// names via errors.Is. A caller error not matching any of the four is a
// bug in this emitter, not a valid case outcome, so it is surfaced as a
// process failure rather than silently coerced into a sentinel string.
func sentinelResult(err error) (any, error) {
	switch {
	case errors.Is(err, hooks.ErrUnknownHost):
		return map[string]any{"error": "ErrUnknownHost"}, nil
	case errors.Is(err, hooks.ErrUnsupportedEvent):
		return map[string]any{"error": "ErrUnsupportedEvent"}, nil
	case errors.Is(err, hooks.ErrUnsupportedAction):
		return map[string]any{"error": "ErrUnsupportedAction"}, nil
	case errors.Is(err, hooks.ErrSchema):
		return map[string]any{"error": "ErrSchema"}, nil
	default:
		return nil, fmt.Errorf("unmapped error (not one of the four sentinels): %w", err)
	}
}

// writeCanonicalJSON writes v to stdout as canonical JSON: keys sorted,
// no insignificant whitespace, one trailing newline. json.Marshal already
// sorts map keys and emits no whitespace; struct field order would not be
// sorted, so every result above is built as a map, never a struct.
func writeCanonicalJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = os.Stdout.Write(b)
	return err
}
