package axon

import (
	"io/fs"
	"testing"
)

func TestHostsCount(t *testing.T) {
	if n := len(Hosts()); n != 17 {
		t.Fatalf("got %d hosts, want 17", n)
	}
}

func TestGetIsCanonicalOnly(t *testing.T) {
	if _, ok := Get("claude-code"); ok {
		t.Fatal("Get accepted an alias; only Resolve may")
	}
	h, ok := Get(HostClaude)
	if !ok || h.Name != "claude" {
		t.Fatalf("Get(claude) = %+v, %v", h, ok)
	}
}

func TestResolveAliases(t *testing.T) {
	for alias, want := range map[string]string{
		"claude-code": "claude", "gemini-cli": "gemini", "codex-cli": "codex", "opencode": "opencode",
	} {
		h, ok := Resolve(alias)
		if !ok || h.Name != want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", alias, h.Name, ok, want)
		}
	}
	if _, ok := Resolve("nope"); ok {
		t.Error("Resolve accepted an unknown name")
	}
}

func TestAliasUniqueness(t *testing.T) {
	seen := map[string]string{}
	for _, h := range Hosts() {
		seen[h.Name] = h.Name
	}
	for _, h := range Hosts() {
		for _, a := range h.Aliases {
			if owner, dup := seen[a]; dup {
				t.Errorf("alias %q of %s collides with %s", a, h.Name, owner)
			}
			seen[a] = h.Name
		}
	}
}

func TestDirectoryNameMatchesHostName(t *testing.T) {
	for _, h := range Hosts() {
		if _, err := fs.Stat(Spec(), "hosts/"+h.Name+"/host.yaml"); err != nil {
			t.Errorf("%s: %v", h.Name, err)
		}
	}
}

func TestHookedHostsAreEight(t *testing.T) {
	if n := len(HookedHosts()); n != 8 {
		t.Fatalf("got %d hooked hosts, want 8", n)
	}
}

func TestGeneratedConstantsMatchHosts(t *testing.T) {
	names := map[string]bool{}
	for _, h := range Hosts() {
		names[h.Name] = true
	}
	if len(allHostNames) != len(names) {
		t.Fatalf("allHostNames has %d entries, want %d", len(allHostNames), len(names))
	}
	for _, n := range allHostNames {
		if !names[n] {
			t.Errorf("allHostNames entry %q has no matching host", n)
		}
		if _, ok := Get(n); !ok {
			t.Errorf("allHostNames entry %q: Get failed", n)
		}
	}
}
