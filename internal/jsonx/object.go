// Package jsonx contains small JSON helpers that encoding/json lacks:
// order-preserving object decoding and encoding.
package jsonx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// DecodeObject walks a JSON object in document order, calling fn for every
// member. Duplicate keys and non-object input are errors.
func DecodeObject(data []byte, fn func(key string, value json.RawMessage) error) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("expected a JSON object")
	}
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := tok.(string)
		if !ok {
			return fmt.Errorf("expected object key")
		}
		if seen[key] {
			return fmt.Errorf("duplicate key %q", key)
		}
		seen[key] = true
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		if err := fn(key, raw); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil { // closing '}'
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("unexpected data after JSON object")
	}
	return nil
}

// ObjectWriter builds a JSON object with members in insertion order.
type ObjectWriter struct {
	buf bytes.Buffer
	n   int
	err error
}

// Add appends key: value (value is marshaled with encoding/json).
func (w *ObjectWriter) Add(key string, value any) {
	if w.err != nil {
		return
	}
	if w.n == 0 {
		w.buf.WriteByte('{')
	} else {
		w.buf.WriteByte(',')
	}
	w.n++
	k, _ := json.Marshal(key)
	w.buf.Write(k)
	w.buf.WriteByte(':')
	v, err := json.Marshal(value)
	if err != nil {
		w.err = err
		return
	}
	w.buf.Write(v)
}

// Bytes returns the encoded object.
func (w *ObjectWriter) Bytes() ([]byte, error) {
	if w.err != nil {
		return nil, w.err
	}
	if w.n == 0 {
		return []byte("{}"), nil
	}
	return append(w.buf.Bytes(), '}'), nil
}
