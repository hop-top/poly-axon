package spectest

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestValidateAllReportsMissingSchemaMapping(t *testing.T) {
	fsys := fstest.MapFS{
		"unmapped.yaml": &fstest.MapFile{Data: []byte("x: 1\n")},
	}
	errs := ValidateAll(fsys, func(fs.FS, string, string) error {
		t.Fatal("validate should not be called for an unmapped path")
		return nil
	})
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want exactly one", errs)
	}
}

func TestValidateAllInvokesValidatorForKnownPaths(t *testing.T) {
	fsys := fstest.MapFS{
		"version.yaml":                   &fstest.MapFile{Data: []byte("version: 0.1.0\n")},
		"events.yaml":                    &fstest.MapFile{Data: []byte("events: []\n")},
		"hosts/claude/host.yaml":         &fstest.MapFile{Data: []byte("name: claude\n")},
		"hosts/claude/capabilities.yaml": &fstest.MapFile{Data: []byte("host: claude\n")},
		"hosts/claude/invoke.yaml":       &fstest.MapFile{Data: []byte("host: claude\n")},
	}

	var got []struct{ path, schema string }
	errs := ValidateAll(fsys, func(_ fs.FS, p, s string) error {
		got = append(got, struct{ path, schema string }{p, s})
		return nil
	})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	want := map[string]string{
		"version.yaml":                   "version.schema.json",
		"events.yaml":                    "events.schema.json",
		"hosts/claude/host.yaml":         "host.schema.json",
		"hosts/claude/capabilities.yaml": "capabilities.schema.json",
		"hosts/claude/invoke.yaml":       "invoke.schema.json",
	}
	if len(got) != len(want) {
		t.Fatalf("validator called %d times, want %d (%v)", len(got), len(want), got)
	}
	for _, g := range got {
		if want[g.path] != g.schema {
			t.Errorf("path %s: schema = %s, want %s", g.path, g.schema, want[g.path])
		}
	}
}

func TestValidateAllPropagatesValidatorError(t *testing.T) {
	fsys := fstest.MapFS{
		"version.yaml": &fstest.MapFile{Data: []byte("version: 0.1.0\n")},
	}
	wantErr := errors.New("boom")
	errs := ValidateAll(fsys, func(fs.FS, string, string) error {
		return wantErr
	})
	if len(errs) != 1 || !errors.Is(errs[0], wantErr) {
		t.Fatalf("errs = %v, want [%v]", errs, wantErr)
	}
}
