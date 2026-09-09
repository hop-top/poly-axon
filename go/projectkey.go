package axon

import (
	"crypto/md5"  //nolint:gosec // non-cryptographic: replicates a host's project_key_strategy enum for interop, not security
	"crypto/sha1" //nolint:gosec // non-cryptographic: replicates a host's project_key_strategy enum for interop, not security
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

// projectKeyStrategies enumerates the project_key_strategy values defined
// by the host schema. DeriveKey rejects any strategy not listed here.
var projectKeyStrategies = map[string]struct{}{
	"slash-to-dash":  {},
	"sha1":           {},
	"sha256":         {},
	"md5":            {},
	"basename-alias": {},
	"embedded":       {},
	"none":           {},
}

// DeriveKey produces a project key from cwd using the given strategy.
// strategy must be one of the host schema's project_key_strategy enum
// values; any other value returns an error rather than a default
// derivation.
func DeriveKey(cwd, strategy string) (string, error) {
	if _, ok := projectKeyStrategies[strategy]; !ok {
		return "", fmt.Errorf("axon: unknown project key strategy %q", strategy)
	}

	switch strategy {
	case "slash-to-dash":
		if cwd == "" {
			return "", nil
		}
		normalized := filepath.ToSlash(filepath.Clean(cwd))
		replaced := strings.ReplaceAll(normalized, "/", "-")
		return strings.TrimPrefix(replaced, "-"), nil
	case "sha1":
		h := sha1.Sum([]byte(cwd)) //nolint:gosec // non-cryptographic: replicates a host's project_key_strategy enum for interop, not security
		return hex.EncodeToString(h[:]), nil
	case "sha256":
		h := sha256.Sum256([]byte(cwd))
		return hex.EncodeToString(h[:]), nil
	case "md5":
		h := md5.Sum([]byte(cwd)) //nolint:gosec // non-cryptographic: replicates a host's project_key_strategy enum for interop, not security
		return hex.EncodeToString(h[:]), nil
	case "basename-alias":
		return filepath.Base(cwd), nil
	case "embedded", "none":
		return cwd, nil
	default:
		// Unreachable: covered by the projectKeyStrategies guard above.
		return "", fmt.Errorf("axon: unknown project key strategy %q", strategy)
	}
}
