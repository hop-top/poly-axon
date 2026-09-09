package hooks

import (
	"fmt"

	"hop.top/axon"
)

// Level classifies how a host supports a canonical event.
type Level string

const (
	LevelNative      Level = "native"
	LevelClose       Level = "close"
	LevelSynthesized Level = "synthesized"
	LevelUnsupported Level = "unsupported"
)

// Pair maps a host-side event name to a canonical event.
type Pair struct {
	HostEvent string     `yaml:"host_event"`
	Event     axon.Event `yaml:"event"`
	Note      string     `yaml:"note,omitempty"`
}

// Recipe describes how a host synthesizes an event it lacks natively.
type Recipe struct {
	Technique string `yaml:"technique,omitempty"`
	Source    string `yaml:"source,omitempty"`
	Via       string `yaml:"via,omitempty"`
	Pattern   string `yaml:"pattern,omitempty"`
	Cost      string `yaml:"cost,omitempty"`
	Notes     string `yaml:"notes,omitempty"`
}

// Capabilities is a host's parsed capabilities.yaml.
type Capabilities struct {
	Host             string                `yaml:"host"`
	HostVersionRange string                `yaml:"host_version_range,omitempty"`
	Native           []Pair                `yaml:"native"`
	Close            []Pair                `yaml:"close,omitempty"`
	Synthesized      map[axon.Event]Recipe `yaml:"synthesized,omitempty"`
	Unsupported      []axon.Event          `yaml:"unsupported,omitempty"`
}

// Level returns the support level for e and whether e is classified at all.
func (c Capabilities) Level(e axon.Event) (Level, bool) {
	for _, p := range c.Native {
		if p.Event == e {
			return LevelNative, true
		}
	}
	for _, p := range c.Close {
		if p.Event == e {
			return LevelClose, true
		}
	}
	if _, ok := c.Synthesized[e]; ok {
		return LevelSynthesized, true
	}
	for _, u := range c.Unsupported {
		if u == e {
			return LevelUnsupported, true
		}
	}
	return "", false
}

// Recipe returns the synthesis recipe for e when it is synthesized.
func (c Capabilities) Recipe(e axon.Event) (Recipe, bool) {
	r, ok := c.Synthesized[e]
	return r, ok
}

// HostEvent returns the host-side event name for a native or close mapping.
func (c Capabilities) HostEvent(e axon.Event) (string, bool) {
	for _, p := range c.Native {
		if p.Event == e {
			return p.HostEvent, true
		}
	}
	for _, p := range c.Close {
		if p.Event == e {
			return p.HostEvent, true
		}
	}
	return "", false
}

// LoadCapabilities loads and validates the embedded capabilities.yaml for a
// hooked host. Returns ErrUnknownHost for a name axon does not recognize
// (canonical names only). A hooked host missing its capabilities.yaml is a
// load error, not an empty Capabilities value.
func LoadCapabilities(host string) (Capabilities, error) {
	if _, ok := axon.Get(host); !ok {
		return Capabilities{}, fmt.Errorf("%w: %q", ErrUnknownHost, host)
	}
	var c Capabilities
	if err := axon.LoadSpecYAML("hosts/"+host+"/capabilities.yaml", "capabilities.schema.json", &c); err != nil {
		return Capabilities{}, err
	}
	return c, nil
}

// DecodeMap builds the host-event -> canonical-event map a codec uses to
// decode a native stdin envelope.
//
// Native rows always win. A Close row is a lossy approximation — a
// canonical event the host has no exact hook for, mapped onto the nearest
// host event it does have — so it may be used to ENCODE a canonical event
// into a host name, but it must never displace a Native row on decode.
// gemini lists host event SessionEnd twice, native -> SessionEnd and
// close -> Stop; only the native row says what the host actually emitted,
// and building the map close-last silently answered Stop. A Close row
// whose host event no Native row claims is unambiguous and does decode
// (opencode's todo.updated -> TaskCompleted and tui.prompt.append ->
// UserPromptSubmit are the only way those envelopes decode at all).
//
// Two rows within one section sharing a host event are unresolvable on
// decode, so they are an error rather than a silent last-row-wins.
func (c Capabilities) DecodeMap() (map[string]axon.Event, error) {
	m := make(map[string]axon.Event, len(c.Native)+len(c.Close))
	for _, p := range c.Native {
		if prev, dup := m[p.HostEvent]; dup {
			return nil, fmt.Errorf("%w: %s: host event %q maps to both %q and %q under native",
				ErrSchema, c.Host, p.HostEvent, prev, p.Event)
		}
		m[p.HostEvent] = p.Event
	}
	seenClose := make(map[string]axon.Event, len(c.Close))
	for _, p := range c.Close {
		if prev, dup := seenClose[p.HostEvent]; dup {
			return nil, fmt.Errorf("%w: %s: host event %q maps to both %q and %q under close",
				ErrSchema, c.Host, p.HostEvent, prev, p.Event)
		}
		seenClose[p.HostEvent] = p.Event
		if _, claimed := m[p.HostEvent]; claimed {
			continue // native wins; the close row stays encode-only
		}
		m[p.HostEvent] = p.Event
	}
	return m, nil
}
