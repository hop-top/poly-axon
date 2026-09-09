package axon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// loadYAML reads path from fsys, validates the decoded document against
// schemaPath (draft-07, also in fsys), then decodes into out. Validation
// runs on a JSON round-trip of the YAML value so numbers and maps match
// what the schema library expects.
func loadYAML(fsys fs.FS, path, schemaPath string, out any) error {
	raw, err := fs.ReadFile(fsys, path)
	if err != nil {
		return fmt.Errorf("axon: read %s: %w", path, err)
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("axon: parse %s: %w", path, err)
	}
	jsonDoc, err := toJSONValue(doc)
	if err != nil {
		return fmt.Errorf("axon: %s: %w", path, err)
	}
	sch, err := compileSchema(fsys, schemaPath)
	if err != nil {
		return err
	}
	if err := sch.Validate(jsonDoc); err != nil {
		return fmt.Errorf("axon: %s violates %s: %w", path, schemaPath, err)
	}
	return yaml.Unmarshal(raw, out)
}

func toJSONValue(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// LoadSpecYAML validates path (relative to the spec root) against schemaPath
// and decodes it into out. Exported for packages that read spec data.
func LoadSpecYAML(path, schemaPath string, out any) error {
	return loadYAML(Spec(), path, schemaPath, out)
}

func compileSchema(fsys fs.FS, schemaPath string) (*jsonschema.Schema, error) {
	raw, err := fs.ReadFile(fsys, schemaPath)
	if err != nil {
		return nil, fmt.Errorf("axon: read schema %s: %w", schemaPath, err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("axon: parse schema %s: %w", schemaPath, err)
	}
	c := jsonschema.NewCompiler()
	url := "axon:///" + schemaPath
	if err := c.AddResource(url, doc); err != nil {
		return nil, err
	}
	return c.Compile(url)
}
