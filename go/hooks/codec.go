package hooks

import (
	"fmt"
	"sort"
	"sync"

	"hop.top/axon"
)

// Codec translates between a host's native hook wire format and the
// canonical Input/Decision types.
type Codec interface {
	Host() string
	EncodeInput(Input) ([]byte, error)
	DecodeInput([]byte) (Input, error)
	EncodeDecision(Decision) (stdout []byte, exit int, err error)
	DecodeDecision(stdout []byte, exit int) (Decision, error)
	Capabilities() Capabilities
}

var (
	regMu  sync.RWMutex
	codecs = map[string]Codec{}
)

// Register adds a codec. It panics on a duplicate host or a host that is
// not in the spec: both are programming errors caught at init.
func Register(c Codec) {
	if _, ok := axon.Get(c.Host()); !ok {
		panic(fmt.Sprintf("axon/hooks: codec for host %q not in spec/hosts", c.Host()))
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := codecs[c.Host()]; dup {
		panic(fmt.Sprintf("axon/hooks: codec for %q registered twice", c.Host()))
	}
	codecs[c.Host()] = c
}

// For returns the codec for a canonical host name.
func For(host string) (Codec, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	c, ok := codecs[host]
	return c, ok
}

// Registered returns the sorted list of hosts with a codec.
func Registered() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(codecs))
	for h := range codecs {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// resetRegistryForTest clears the registry and returns a func that
// restores the snapshot taken before clearing. Tests must defer the
// returned func (or register it via t.Cleanup) so codecs registered by
// hooks/hosts's init — via another test file's "_ hop.top/axon/hooks/hosts"
// import compiled into the same test binary — are not lost for tests that
// run afterward.
func resetRegistryForTest() func() {
	regMu.Lock()
	defer regMu.Unlock()
	prev := codecs
	codecs = map[string]Codec{}
	return func() {
		regMu.Lock()
		defer regMu.Unlock()
		codecs = prev
	}
}
