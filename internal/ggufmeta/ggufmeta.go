// Package ggufmeta opens GGUF headers via github.com/abrander/gguf with a
// buffered reader. It only exposes metadata; nothing here runs inference.
package ggufmeta

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"github.com/abrander/gguf"
)

// Metadata is the key/value metadata of a GGUF file.
type Metadata = gguf.Metadata

// ReadFile reads the metadata of a GGUF file.
func ReadFile(path string) (Metadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r, err := gguf.Open(newBufferedReadSeeker(f))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return r.Metadata, nil
}

// Has reports whether key exists.
func Has(m Metadata, key string) bool {
	_, ok := m[key]
	return ok
}

// String returns a string value or "".
func String(m Metadata, key string) string {
	s, _ := m.String(key)
	return s
}

// Int returns a numeric value or def.
func Int(m Metadata, key string, def int) int {
	v, err := m.Int(key)
	if err != nil {
		return def
	}
	return v
}

// bufferedReadSeeker adds read buffering to an io.ReadSeeker; abrander/gguf
// issues many tiny reads while decoding tokenizer arrays.
type bufferedReadSeeker struct {
	rs  io.ReadSeeker
	br  *bufio.Reader
	pos int64 // logical position
}

func newBufferedReadSeeker(rs io.ReadSeeker) *bufferedReadSeeker {
	return &bufferedReadSeeker{rs: rs, br: bufio.NewReaderSize(rs, 1<<20)}
}

func (b *bufferedReadSeeker) Read(p []byte) (int, error) {
	n, err := b.br.Read(p)
	b.pos += int64(n)
	return n, err
}

func (b *bufferedReadSeeker) Seek(offset int64, whence int) (int64, error) {
	var target int64
	switch whence {
	case io.SeekStart:
		target = offset
	case io.SeekCurrent:
		target = b.pos + offset
	default:
		n, err := b.rs.Seek(offset, whence)
		if err == nil {
			b.pos = n
			b.br.Reset(b.rs)
		}
		return n, err
	}
	// Forward seeks within the buffer avoid a syscall.
	if d := target - b.pos; d >= 0 && d <= int64(b.br.Buffered()) {
		if _, err := b.br.Discard(int(d)); err != nil {
			return b.pos, err
		}
		b.pos = target
		return target, nil
	}
	n, err := b.rs.Seek(target, io.SeekStart)
	if err != nil {
		return b.pos, err
	}
	b.pos = n
	b.br.Reset(b.rs)
	return n, nil
}
