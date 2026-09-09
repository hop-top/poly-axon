// Order-preserving JSON decoding for capture.
//
// encoding/json decodes an object into a map, which loses the order its
// keys were written in — and that order is part of the shape a fixture
// records: the committed tool_response objects read
// {"stdout": ..., "stderr": ...}, which is the host's order and not the
// alphabet's. The types below read the same JSON while remembering it.

package capture

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// object is a JSON object that remembers the order its keys were written
// in. encoding/json's map decode loses that, and a nested object's key
// order is part of the shape a fixture records: the committed
// tool_response objects read {"stdout": ..., "stderr": ...}, which is the
// host's order, not the alphabet's.
type object struct {
	keys   []string
	values map[string]any
}

func (o *object) get(k string) any { return o.values[k] }

// set replaces an existing key's value in place, or appends a new key.
func (o *object) set(k string, v any) {
	if _, exists := o.values[k]; !exists {
		o.keys = append(o.keys, k)
	}
	o.values[k] = v
}

// decodeObject parses a JSON object, preserving key order at every depth.
// Numbers are kept as json.Number so a large integer or a trailing zero
// survives the round trip exactly as the host wrote it.
func decodeObject(raw []byte) (*object, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if tok != json.Delim('{') {
		return nil, fmt.Errorf("top-level value is %v, want an object", tok)
	}
	obj, err := readObject(dec)
	if err != nil {
		return nil, err
	}
	// Reject trailing content, so a truncated or concatenated read is a
	// failure rather than a silently half-recorded envelope.
	if _, err := dec.Token(); err == nil {
		return nil, fmt.Errorf("trailing content after the envelope")
	}
	return obj, nil
}

// readObject reads an object's members after its opening brace has been
// consumed.
func readObject(dec *json.Decoder) (*object, error) {
	obj := &object{values: map[string]any{}}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("object key is %v, want a string", keyTok)
		}
		v, err := readValue(dec)
		if err != nil {
			return nil, err
		}
		obj.set(key, v)
	}
	if _, err := dec.Token(); err != nil { // closing brace
		return nil, err
	}
	return obj, nil
}

// readValue reads one JSON value, recursing into objects and arrays so
// nested key order is preserved throughout.
func readValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch tok {
	case json.Delim('{'):
		return readObject(dec)
	case json.Delim('['):
		var items []any
		for dec.More() {
			v, err := readValue(dec)
			if err != nil {
				return nil, err
			}
			items = append(items, v)
		}
		if _, err := dec.Token(); err != nil { // closing bracket
			return nil, err
		}
		if items == nil {
			items = []any{}
		}
		return items, nil
	default:
		return tok, nil
	}
}
