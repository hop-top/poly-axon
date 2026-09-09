package axon

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
)

// ExecRunner executes a named program with arguments and returns its
// combined output. This is the testable seam for CLI detection.
type ExecRunner interface {
	Run(name string, args ...string) ([]byte, error)
}

// LookPather resolves a binary name to its full path. This is the
// testable seam for exec.LookPath.
type LookPather interface {
	LookPath(file string) (string, error)
}

// DefaultExecRunner runs commands via os/exec.
type DefaultExecRunner struct{}

// Run executes the program and returns combined output.
func (d *DefaultExecRunner) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput() //nolint:gosec // ExecRunner is a generic exec seam by design; callers own the input
}

// DefaultLookPather uses exec.LookPath.
type DefaultLookPather struct{}

// LookPath resolves the binary path.
func (d *DefaultLookPather) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

// DetectOpts configures Detect behavior. Nil fields use defaults.
type DetectOpts struct {
	Runner   ExecRunner
	LookPath LookPather
}

// DetectResult captures what Detect found about a host installation.
type DetectResult struct {
	Installed   bool     `json:"installed"`
	Version     string   `json:"version,omitempty"`
	BinaryPath  string   `json:"binary_path,omitempty"`
	ConfigPaths []string `json:"config_paths,omitempty"`
	Errors      []string `json:"errors,omitempty"`
}

// Detect probes the local environment for a host's presence.
//
// It walks the host's Binaries looking for the first binary that responds
// to --version, parses the version string, and checks whether the store
// root directory exists via ResolveStorePath. name must be a canonical
// host name known to Get; an unknown name returns an error wrapping
// ErrUnknownHost.
func Detect(name string, opts *DetectOpts) (*DetectResult, error) {
	h, ok := Get(name)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownHost, name)
	}

	runner := ExecRunner(&DefaultExecRunner{})
	lookpath := LookPather(&DefaultLookPather{})
	if opts != nil {
		if opts.Runner != nil {
			runner = opts.Runner
		}
		if opts.LookPath != nil {
			lookpath = opts.LookPath
		}
	}

	result := &DetectResult{}

	// Try each binary name in order.
	var foundBinary string
	for _, bin := range h.Binaries {
		out, err := runner.Run(bin, "--version")
		if err != nil {
			result.Errors = append(result.Errors,
				fmt.Sprintf("%s --version: %v", bin, err))
			continue
		}
		foundBinary = bin
		result.Version = parseVersion(string(out))
		break
	}

	if foundBinary == "" {
		return result, nil
	}

	result.Installed = true

	// Resolve binary path via injected LookPath.
	if path, err := lookpath.LookPath(foundBinary); err == nil {
		result.BinaryPath = path
	}

	// Check store root existence via ResolveStorePath (non-fatal).
	if p, err := ResolveStorePath(name); err == nil {
		if _, statErr := os.Stat(p); statErr != nil {
			result.Errors = append(result.Errors,
				fmt.Sprintf("store path %s: %v", p, statErr))
		}
	}

	return result, nil
}

// versionRe matches semver-like version strings, optionally prefixed
// with "v" and optionally followed by a pre-release tag.
var versionRe = regexp.MustCompile(`v?(\d+\.\d+\.\d+(?:-[a-zA-Z0-9.]+)?)`)

// parseVersion extracts the first semver-like version from raw
// --version output.
func parseVersion(raw string) string {
	m := versionRe.FindStringSubmatch(raw)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}
