package axon

import "sync"

// Event is a canonical hook event name from spec/events.yaml.
type Event string

// Derivation describes how a derived event is synthesized from a native one.
type Derivation struct {
	From Event          `yaml:"from"`
	When map[string]any `yaml:"when"`
}

// EventInfo is one catalog entry.
type EventInfo struct {
	Name            Event       `yaml:"name"`
	Category        string      `yaml:"category"`
	Description     string      `yaml:"description"`
	Direction       string      `yaml:"direction"`
	Blocking        bool        `yaml:"blocking"`
	Origin          string      `yaml:"origin"` // "" means native
	ExtensionSource string      `yaml:"extension_source"`
	Derivation      *Derivation `yaml:"derivation"`
}

// EffectiveOrigin returns Origin, or "native" when blank.
func (e EventInfo) EffectiveOrigin() string {
	if e.Origin == "" {
		return "native"
	}
	return e.Origin
}

var (
	eventsOnce sync.Once
	eventsList []EventInfo
	eventsErr  error
)

func loadEvents() {
	var doc struct {
		Events []EventInfo `yaml:"events"`
	}
	eventsErr = loadYAML(Spec(), "events.yaml", "events.schema.json", &doc)
	eventsList = doc.Events
}

// Events returns the full catalog in file order. Panics only if the
// embedded file is corrupt, which the schema test prevents.
func Events() []EventInfo {
	eventsOnce.Do(loadEvents)
	if eventsErr != nil {
		panic(eventsErr)
	}
	out := make([]EventInfo, len(eventsList))
	copy(out, eventsList)
	return out
}

// NativeEvents returns the host-contract scope: events a host CLI can emit
// directly. Origin is the whole test -- an extension event is native to one
// CLI but not all, and a derived event is synthesized rather than emitted, so
// both are excluded. 26 of the 32 catalog entries qualify, which is the
// capability matrix denominator.
func NativeEvents() []Event {
	var out []Event
	for _, e := range Events() {
		if e.EffectiveOrigin() == "native" {
			out = append(out, e.Name)
		}
	}
	return out
}
