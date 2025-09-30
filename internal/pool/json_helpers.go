package pool

import (
	"bytes"
	"fmt"
	"io"
	"sync"

	json "github.com/goccy/go-json"
)

// JSONEncoder provides a pooled buffer-backed JSON encoder for repeated marshal operations.
type JSONEncoder struct {
	buf *bytes.Buffer
}

var jsonEncoderPool = sync.Pool{
	New: func() any {
		return &JSONEncoder{buf: bytes.NewBuffer(make([]byte, 0, 2048))}
	},
}

// AcquireJSONEncoder returns a pooled JSON encoder.
func AcquireJSONEncoder() *JSONEncoder {
	enc := jsonEncoderPool.Get().(*JSONEncoder)
	enc.buf.Reset()
	return enc
}

// Encode marshals the value into the internal buffer and returns a copy of the bytes.
// The returned slice is safe for use after the encoder is released.
func (e *JSONEncoder) Encode(v any) ([]byte, error) {
	if e == nil {
		return nil, nil
	}
	e.buf.Reset()
	encoder := json.NewEncoder(e.buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return nil, fmt.Errorf("json encode: %w", err)
	}
	data := e.buf.Bytes()
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}

// WriteTo writes the encoded bytes directly to the provided writer without allocating a copy.
func (e *JSONEncoder) WriteTo(w io.Writer, v any) error {
	if e == nil {
		return nil
	}
	e.buf.Reset()
	encoder := json.NewEncoder(e.buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return fmt.Errorf("json encode: %w", err)
	}
	data := e.buf.Bytes()
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write encoded json: %w", err)
	}
	return nil
}

// ReleaseJSONEncoder returns the encoder to the pool.
func ReleaseJSONEncoder(enc *JSONEncoder) {
	if enc == nil {
		return
	}
	enc.buf.Reset()
	jsonEncoderPool.Put(enc)
}
