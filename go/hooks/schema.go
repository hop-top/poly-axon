package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"hop.top/axon"
)

func ValidateInput(host string, ev axon.Event, raw []byte) error {
	return validate(host, fmt.Sprintf("hosts/%s/hooks/%s.input.schema.json", host, ev), raw)
}

func ValidateDecision(host string, ev axon.Event, action Action, raw []byte) error {
	return validate(host, fmt.Sprintf("hosts/%s/hooks/%s.%s.schema.json", host, ev, action), raw)
}

func validate(host, schemaPath string, raw []byte) error {
	if _, ok := axon.Get(host); !ok {
		return fmt.Errorf("%w: %q", ErrUnknownHost, host)
	}
	sraw, err := fs.ReadFile(axon.Spec(), schemaPath)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrSchema, schemaPath, err)
	}
	sdoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(sraw))
	if err != nil {
		return fmt.Errorf("%w: parse %s: %v", ErrSchema, schemaPath, err)
	}
	c := jsonschema.NewCompiler()
	url := "axon:///" + schemaPath
	if err := c.AddResource(url, sdoc); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrSchema, schemaPath, err)
	}
	sch, err := c.Compile(url)
	if err != nil {
		return fmt.Errorf("%w: compile %s: %v", ErrSchema, schemaPath, err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("%w: %s: payload is not JSON: %v", ErrSchema, schemaPath, err)
	}
	if err := sch.Validate(doc); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrSchema, schemaPath, err)
	}
	return nil
}
