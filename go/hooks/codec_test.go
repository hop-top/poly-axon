package hooks

import (
	"testing"

	"hop.top/axon"
)

type fakeCodec struct{ host string }

func (f fakeCodec) Host() string                                 { return f.host }
func (f fakeCodec) EncodeInput(Input) ([]byte, error)            { return []byte(`{}`), nil }
func (f fakeCodec) DecodeInput([]byte) (Input, error)            { return Input{}, nil }
func (f fakeCodec) EncodeDecision(Decision) ([]byte, int, error) { return nil, 0, nil }
func (f fakeCodec) DecodeDecision([]byte, int) (Decision, error) { return Decision{}, nil }
func (f fakeCodec) Capabilities() Capabilities                   { return Capabilities{Host: f.host} }

func TestRegisterAndFor(t *testing.T) {
	defer resetRegistryForTest()()
	Register(fakeCodec{host: axon.HostClaude})
	if _, ok := For(axon.HostClaude); !ok {
		t.Fatal("registered codec not found")
	}
	if _, ok := For("claude-code"); ok {
		t.Fatal("For accepted an alias; only axon.Resolve may")
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	defer resetRegistryForTest()()
	Register(fakeCodec{host: axon.HostGemini})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	Register(fakeCodec{host: axon.HostGemini})
}

func TestRegisterUnknownHostPanics(t *testing.T) {
	defer resetRegistryForTest()()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for host not in spec")
		}
	}()
	Register(fakeCodec{host: "nope"})
}
