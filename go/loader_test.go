package axon

import (
	"io/fs"
	"regexp"
	"testing"

	"hop.top/axon/internal/spectest"
)

func TestSpecFSHasVersion(t *testing.T) {
	b, err := fs.ReadFile(Spec(), "version.yaml")
	if err != nil {
		t.Fatalf("version.yaml: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("version.yaml is empty")
	}
}

func TestLoadYAMLRejectsSchemaViolation(t *testing.T) {
	var out struct {
		Version string `yaml:"version"`
	}
	err := loadYAML(Spec(), "version.yaml", "version.schema.json", &out)
	if err != nil {
		t.Fatalf("valid file rejected: %v", err)
	}
	// Assert the field was actually populated and is a semver, not a
	// specific release: this test covers loadYAML, and pinning the literal
	// turned every legitimate spec bump into a failure here.
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(out.Version) {
		t.Fatalf("version = %q, want a semver like 0.1.0", out.Version)
	}
}

func TestEverySpecFileValidates(t *testing.T) {
	errs := spectest.ValidateAll(Spec(), func(fsys fs.FS, p, s string) error {
		var sink any
		return loadYAML(fsys, p, s, &sink)
	})
	for _, e := range errs {
		t.Error(e)
	}
}
