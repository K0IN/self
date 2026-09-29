// Package ipc implements the small framed binary protocol used between the
// Go server (adapter side) and native engine subprocesses over stdin/stdout.
//
// Wire format (all integers big-endian):
//
//	"SELFIPC1"                 8 bytes magic + version
//	uint32 header_length
//	<header JSON>              header_length bytes
//	uint32 attachment_count
//	for each attachment:
//	    uint64 attachment_length
//	    <attachment bytes>
//
// Requests and responses use the same framing. Binary payloads (for example
// RGB pixels) travel as attachments and are never base64-encoded.
package ipc

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Magic identifies protocol version 1 frames.
const Magic = "SELFIPC1"

// Limits bound the size of frames accepted from a peer.
type Limits struct {
	MaxHeader      uint32
	MaxAttachments uint32
	MaxAttachment  uint64
	MaxTotal       uint64
}

// DefaultLimits are suitable for decision requests with a few images.
func DefaultLimits() Limits {
	return Limits{
		MaxHeader:      4 << 20,
		MaxAttachments: 32,
		MaxAttachment:  128 << 20,
		MaxTotal:       512 << 20,
	}
}

// Frame is one message.
type Frame struct {
	Header      json.RawMessage
	Attachments [][]byte
}

var (
	ErrBadMagic  = errors.New("ipc: bad frame magic")
	ErrTooLarge  = errors.New("ipc: frame exceeds size limit")
	ErrBadHeader = errors.New("ipc: malformed frame header")
)

// Writer writes frames to an underlying stream.
type Writer struct {
	w   *bufio.Writer
	lim Limits
}

// NewWriter returns a frame writer.
func NewWriter(w io.Writer, lim Limits) *Writer {
	return &Writer{w: bufio.NewWriterSize(w, 1<<16), lim: lim}
}

// WriteFrame validates and writes f, then flushes.
func (w *Writer) WriteFrame(f Frame) error {
	if err := validateOutgoing(f, w.lim); err != nil {
		return err
	}
	var hdr [12]byte
	copy(hdr[:8], Magic)
	binary.BigEndian.PutUint32(hdr[8:], uint32(len(f.Header)))
	if _, err := w.w.Write(hdr[:]); err != nil {
		return err
	}
	if _, err := w.w.Write(f.Header); err != nil {
		return err
	}
	var n [8]byte
	binary.BigEndian.PutUint32(n[:4], uint32(len(f.Attachments)))
	if _, err := w.w.Write(n[:4]); err != nil {
		return err
	}
	for _, a := range f.Attachments {
		binary.BigEndian.PutUint64(n[:], uint64(len(a)))
		if _, err := w.w.Write(n[:]); err != nil {
			return err
		}
		if _, err := w.w.Write(a); err != nil {
			return err
		}
	}
	return w.w.Flush()
}

func validateOutgoing(f Frame, lim Limits) error {
	if len(f.Header) == 0 || !json.Valid(f.Header) {
		return ErrBadHeader
	}
	if uint64(len(f.Header)) > uint64(lim.MaxHeader) || uint64(len(f.Attachments)) > uint64(lim.MaxAttachments) {
		return ErrTooLarge
	}
	total := uint64(len(f.Header))
	for _, a := range f.Attachments {
		if uint64(len(a)) > lim.MaxAttachment {
			return ErrTooLarge
		}
		total += uint64(len(a))
	}
	if total > lim.MaxTotal {
		return ErrTooLarge
	}
	return nil
}

// Reader reads frames from an underlying stream.
type Reader struct {
	r   *bufio.Reader
	lim Limits
}

// NewReader returns a frame reader.
func NewReader(r io.Reader, lim Limits) *Reader {
	return &Reader{r: bufio.NewReaderSize(r, 1<<16), lim: lim}
}

// ReadFrame reads one frame. io.EOF is returned only when the stream ends
// cleanly between frames; a partial frame yields io.ErrUnexpectedEOF.
func (r *Reader) ReadFrame() (Frame, error) {
	var hdr [12]byte
	if _, err := io.ReadFull(r.r, hdr[:]); err != nil {
		return Frame{}, err // io.EOF when nothing was read
	}
	if string(hdr[:8]) != Magic {
		return Frame{}, ErrBadMagic
	}
	hlen := binary.BigEndian.Uint32(hdr[8:])
	if hlen == 0 {
		return Frame{}, ErrBadHeader
	}
	if hlen > r.lim.MaxHeader {
		return Frame{}, fmt.Errorf("%w: header %d bytes", ErrTooLarge, hlen)
	}
	header := make([]byte, hlen)
	if err := readFull(r.r, header); err != nil {
		return Frame{}, err
	}
	if !json.Valid(header) {
		return Frame{}, ErrBadHeader
	}
	var nb [8]byte
	if err := readFull(r.r, nb[:4]); err != nil {
		return Frame{}, err
	}
	count := binary.BigEndian.Uint32(nb[:4])
	if count > r.lim.MaxAttachments {
		return Frame{}, fmt.Errorf("%w: %d attachments", ErrTooLarge, count)
	}
	f := Frame{Header: header}
	total := uint64(hlen)
	for i := uint32(0); i < count; i++ {
		if err := readFull(r.r, nb[:]); err != nil {
			return Frame{}, err
		}
		alen := binary.BigEndian.Uint64(nb[:])
		if alen > r.lim.MaxAttachment || total+alen > r.lim.MaxTotal {
			return Frame{}, fmt.Errorf("%w: attachment %d is %d bytes", ErrTooLarge, i, alen)
		}
		total += alen
		a := make([]byte, alen)
		if err := readFull(r.r, a); err != nil {
			return Frame{}, err
		}
		f.Attachments = append(f.Attachments, a)
	}
	return f, nil
}

func readFull(r io.Reader, b []byte) error {
	_, err := io.ReadFull(r, b)
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}
