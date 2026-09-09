package axon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveStorePathTildeExpansion(t *testing.T) {
	got, err := ResolveStorePath(HostClaude)
	if err != nil {
		t.Fatalf("ResolveStorePath(claude): %v", err)
	}

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".claude", "projects")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveStorePathAntigravity(t *testing.T) {
	got, err := ResolveStorePath(HostAntigravity)
	if err != nil {
		t.Fatalf("ResolveStorePath(antigravity): %v", err)
	}

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, "Library", "Application Support", "Antigravity")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveStorePathEnvVar(t *testing.T) {
	got, err := expandStorePath("$HOME/.testcli/data/")
	if err != nil {
		t.Fatalf("expandStorePath: %v", err)
	}

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".testcli", "data")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveStorePathUnknownCLI(t *testing.T) {
	_, err := ResolveStorePath("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown host")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should mention host name, got: %v", err)
	}
}

func TestResolveStorePathAbsolute(t *testing.T) {
	got, err := ResolveStorePath(HostClaude)
	if err != nil {
		t.Fatalf("ResolveStorePath(claude): %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("result should be absolute, got %q", got)
	}
}

// Regression: tilde expansion must land under home dir, not at root.
// Bug was: p[1:] on "~/.foo" yields "/.foo" which is absolute and
// filepath.Join(home, "/.foo") discards home entirely.
func TestResolveStorePath_TildeNotRoot(t *testing.T) {
	got, err := expandStorePath("~/.tildetest/data/")
	if err != nil {
		t.Fatalf("expandStorePath: %v", err)
	}

	home, _ := os.UserHomeDir()
	if !strings.HasPrefix(got, home) {
		t.Errorf("tilde path should start with home %q, got %q", home, got)
	}
	if got == "/.tildetest/data" {
		t.Error("tilde expansion produced root-relative path (regression)")
	}
}

// Regression: bare "~" must resolve to home dir without panic.
func TestResolveStorePath_BareTilde(t *testing.T) {
	got, err := expandStorePath("~")
	if err != nil {
		t.Fatalf("expandStorePath: %v", err)
	}

	home, _ := os.UserHomeDir()
	if got != home {
		t.Errorf("bare ~ should resolve to %q, got %q", home, got)
	}
}

// Regression: relative paths must still produce absolute results.
func TestResolveStorePath_RelativeBecomesAbsolute(t *testing.T) {
	got, err := expandStorePath("relative/path")
	if err != nil {
		t.Fatalf("expandStorePath: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("relative input must produce absolute output, got %q", got)
	}
}

// Regression: an unknown host must produce an error, not a bad default
// derivation.
func TestResolveStorePath_UnknownHostWrapsSentinel(t *testing.T) {
	_, err := ResolveStorePath("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown host")
	}
	if !errors.Is(err, ErrUnknownHost) {
		t.Errorf("error should wrap ErrUnknownHost, got: %v", err)
	}
}
