package axon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveStorePath returns the absolute filesystem path for the given
// host's primary data store. It expands ~ to the user's home directory
// and substitutes environment variables ($VAR / ${VAR}). It does NOT
// verify that the path exists on disk. name must be a canonical host
// name known to Get; an unknown name returns an error wrapping
// ErrUnknownHost.
func ResolveStorePath(name string) (string, error) {
	h, ok := Get(name)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownHost, name)
	}

	p := h.StoreRoots.Data
	if p == "" {
		return "", fmt.Errorf("axon: host %q has no data store path", name)
	}

	return expandStorePath(p)
}

// expandStorePath expands environment variables and a leading ~ in p,
// then cleans and absolutizes the result. It does NOT verify that the
// path exists on disk. Split out from ResolveStorePath so the expansion
// mechanics are testable independent of the host registry.
func expandStorePath(p string) (string, error) {
	// Expand environment variables first (before tilde, so $HOME works).
	p = os.ExpandEnv(p)

	// Expand leading ~/ to home directory.
	if strings.HasPrefix(p, "~/") || p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("axon: resolve home dir: %w", err)
		}
		if p == "~" {
			p = home
		} else {
			p = filepath.Join(home, p[2:])
		}
	}

	// Clean the path to resolve any .. or double separators.
	p = filepath.Clean(p)

	// Ensure absolute path.
	if !filepath.IsAbs(p) {
		p, _ = filepath.Abs(p)
	}

	return p, nil
}
