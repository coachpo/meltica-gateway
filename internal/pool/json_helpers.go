package pool

import (
	"bytes"
	"fmt"
	"io"

	json "github.com/goccy/go-json"
)

// EncodeJSON marshals the value to JSON bytes without HTML escaping.
func EncodeJSON(v any) ([]byte, error) {
	buf := &bytes.Buffer{}
	encoder := json.NewEncoder(buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return nil, fmt.Errorf("json encode: %w", err)
	}
	data := buf.Bytes()
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	return data, nil
}

// WriteJSON encodes and writes JSON directly to the writer without HTML escaping.
func WriteJSON(w io.Writer, v any) error {
	buf := &bytes.Buffer{}
	encoder := json.NewEncoder(buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return fmt.Errorf("json encode: %w", err)
	}
	data := buf.Bytes()
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write encoded json: %w", err)
	}
	return nil
}
