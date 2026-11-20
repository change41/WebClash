package util

import (
	"bytes"
	"encoding/json"
)

// MarshalJSON encodes v as compact JSON without HTML escaping, matching
// Node's JSON.stringify (Fastify response bodies).
func MarshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// MarshalJSONIndent encodes v as 2-space-indented JSON without HTML escaping
// and without a trailing newline, matching JSON.stringify(v, null, 2) used for
// on-disk settings/profiles files.
func MarshalJSONIndent(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
