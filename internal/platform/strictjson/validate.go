// Package strictjson validates bounded JSON before a typed decoder consumes it.
package strictjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const MaxBytes = 256 * 1024
const MaxDepth = 64
const MaxNodes = 20000

// Validate rejects duplicate object keys, excessive structure and trailing data.
// Numbers remain json.Number so validation never rounds an integer.
func Validate(input []byte) error {
	if len(input) > MaxBytes {
		return fmt.Errorf("JSON exceeds %d UTF-8 bytes", MaxBytes)
	}
	d := json.NewDecoder(bytes.NewReader(input))
	d.UseNumber()
	nodes := 0
	var value func(int) error
	value = func(depth int) error {
		if depth > MaxDepth {
			return fmt.Errorf("JSON exceeds nesting depth %d", MaxDepth)
		}
		nodes++
		if nodes > MaxNodes {
			return fmt.Errorf("JSON exceeds %d values", MaxNodes)
		}
		t, err := d.Token()
		if err != nil {
			return err
		}
		switch t {
		case json.Delim('{'):
			seen := make(map[string]struct{})
			for d.More() {
				k, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := k.(string)
				if !ok {
					return fmt.Errorf("object key must be a string")
				}
				if _, exists := seen[key]; exists {
					return fmt.Errorf("duplicate JSON key %q", key)
				}
				seen[key] = struct{}{}
				if err := value(depth + 1); err != nil {
					return err
				}
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return fmt.Errorf("unterminated JSON object")
			}
		case json.Delim('['):
			for d.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return fmt.Errorf("unterminated JSON array")
			}
		case json.Delim('}'), json.Delim(']'):
			return fmt.Errorf("unexpected JSON delimiter")
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("exactly one JSON value required")
	}
	return nil
}
