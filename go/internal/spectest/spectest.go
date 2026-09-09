// Package spectest validates every YAML document under an axon spec tree
// against the schema its path implies. Used by the root package tests
// and by the conformance test; bindings may port the same walk.
package spectest

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// Validator is the root package's loadYAML with `out` discarded.
type Validator func(fsys fs.FS, filePath, schemaPath string) error

// ValidateAll returns one error per YAML file that fails its schema or
// has no schema for its position in the tree.
func ValidateAll(fsys fs.FS, validate Validator) []error {
	var errs []error
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".yaml") {
			return err
		}
		schema, ok := schemaFor(p)
		if !ok {
			errs = append(errs, fmt.Errorf("%s: no schema for this path", p))
			return nil
		}
		if verr := validate(fsys, p, schema); verr != nil {
			errs = append(errs, verr)
		}
		return nil
	})
	return errs
}

func schemaFor(p string) (string, bool) {
	switch {
	case p == "version.yaml":
		return "version.schema.json", true
	case p == "events.yaml":
		return "events.schema.json", true
	case strings.HasPrefix(p, "hosts/") && path.Base(p) == "host.yaml":
		return "host.schema.json", true
	case strings.HasPrefix(p, "hosts/") && path.Base(p) == "capabilities.yaml":
		return "capabilities.schema.json", true
	case strings.HasPrefix(p, "hosts/") && path.Base(p) == "invoke.yaml":
		return "invoke.schema.json", true
	}
	return "", false
}
