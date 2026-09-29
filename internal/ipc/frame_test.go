package ipc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func roundTrip(t *testing.T, f Frame) Frame {
	t.Helper()
	var buf bytes.Buffer
	if err := NewWriter(&buf, DefaultLimits()).WriteFrame(f); err != nil {
		t.Fatal(err)
	}
	got, err := NewReader(&buf, DefaultLimits()).ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestRoundTrip(t *testing.T) {
	got := roundTrip(t, Frame{Header: []byte(`{"id":1,"method":"systemone"}`)})
	if string(got.Header) != `{"id":1,"method":"systemone"}` || len(got.Attachments) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestMultipleAttachments(t *testing.T) {
	a := bytes.Repeat([]byte{1, 2, 3}, 1000)
	b := []byte{}
	c := []byte{0xff}
	got := roundTrip(t, Frame{Header: []byte(`{}`), Attachments: [][]byte{a, b, c}})
	if len(got.Attachments) != 3 || !bytes.Equal(got.Attachments[0], a) || len(got.Attachments[1]) != 0 || got.Attachments[2][0] != 0xff {
		t.Fatal("attachments mismatch")
	}
}

func TestSequentialFramesAndEOF(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf, DefaultLimits())
	for i := 0; i < 3; i++ {
		if err := w.WriteFrame(Frame{Header: []byte(`{"n":1}`)}); err != nil {
			t.Fatal(err)
		}
	}
	r := NewReader(&buf, DefaultLimits())
	for i := 0; i < 3; i++ {
		if _, err := r.ReadFrame(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.ReadFrame(); err != io.EOF {
		t.Fatalf("want EOF, got %v", err)
	}
}

func encoded(t *testing.T, f Frame) []byte {
	var buf bytes.Buffer
	if err := NewWriter(&buf, DefaultLimits()).WriteFrame(f); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestTruncatedFrame(t *testing.T) {
	data := encoded(t, Frame{Header: []byte(`{"id":1}`), Attachments: [][]byte{make([]byte, 100)}})
	for _, cut := range []int{5, 12, 15, 22, len(data) - 1} {
		_, err := NewReader(bytes.NewReader(data[:cut]), DefaultLimits()).ReadFrame()
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("cut %d: got %v", cut, err)
		}
	}
}

func TestOversized(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxAttachment = 10
	data := encoded(t, Frame{Header: []byte(`{}`), Attachments: [][]byte{make([]byte, 11)}})
	if _, err := NewReader(bytes.NewReader(data), lim).ReadFrame(); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("got %v", err)
	}
	if err := NewWriter(io.Discard, lim).WriteFrame(Frame{Header: []byte(`{}`), Attachments: [][]byte{make([]byte, 11)}}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("writer got %v", err)
	}
	// Header length claiming 4 GiB must be rejected before allocation.
	var hdr bytes.Buffer
	hdr.WriteString(Magic)
	binary.Write(&hdr, binary.BigEndian, uint32(0xffffffff))
	if _, err := NewReader(&hdr, DefaultLimits()).ReadFrame(); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("huge header: %v", err)
	}
}

func TestMalformedHeader(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString(Magic)
	binary.Write(&buf, binary.BigEndian, uint32(5))
	buf.WriteString("{nope")
	binary.Write(&buf, binary.BigEndian, uint32(0))
	if _, err := NewReader(&buf, DefaultLimits()).ReadFrame(); !errors.Is(err, ErrBadHeader) {
		t.Fatalf("got %v", err)
	}
	if _, err := NewReader(bytes.NewReader([]byte("NOTMAGIC\x00\x00\x00\x02{}")), DefaultLimits()).ReadFrame(); !errors.Is(err, ErrBadMagic) {
		t.Fatalf("magic: %v", err)
	}
	if err := NewWriter(io.Discard, DefaultLimits()).WriteFrame(Frame{Header: []byte("nope")}); !errors.Is(err, ErrBadHeader) {
		t.Fatalf("writer: %v", err)
	}
}

func TestResponseID(t *testing.T) {
	ok := Frame{Header: []byte(`{"id":7,"result":{}}`)}
	if _, err := ParseResponse(ok, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseResponse(ok, 8); err == nil {
		t.Fatal("mismatched id accepted")
	}
	for _, h := range []string{`{"result":{}}`, `{"id":0,"result":{}}`, `{"id":"7","result":{}}`, `{"id":7}`, `{"id":7,"result":{},"error":{"type":"x"}}`} {
		if _, err := ParseResponse(Frame{Header: []byte(h)}, 7); err == nil {
			t.Errorf("accepted %s", h)
		}
	}
	if _, err := ParseRequest(Frame{Header: []byte(`{"id":0,"method":"x"}`)}); err == nil {
		t.Fatal("zero request id accepted")
	}
}
