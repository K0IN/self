package ggufmeta

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/abrander/gguf"
)

// ReadRemote reads the metadata of a GGUF over HTTP using range requests, so
// only the header (typically a few MiB) is downloaded, never the weights.
func ReadRemote(ctx context.Context, client *http.Client, url, token string) (Metadata, error) {
	rs := &rangeReader{ctx: ctx, client: client, url: url, token: token, blocks: map[int64][]byte{}}
	if err := rs.init(); err != nil {
		return nil, err
	}
	r, err := gguf.Open(rs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	return r.Metadata, nil
}

const blockSize = 1 << 20

// rangeReader is an io.ReadSeeker over HTTP range requests with a block cache.
type rangeReader struct {
	ctx    context.Context
	client *http.Client
	url    string
	token  string
	size   int64
	pos    int64
	blocks map[int64][]byte
}

func (r *rangeReader) get(from, to int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", from, to))
	req.Header.Set("User-Agent", "self-ai-server/1")
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: range request returned HTTP %d", r.url, resp.StatusCode)
	}
	return resp, nil
}

func (r *rangeReader) init() error {
	resp, err := r.get(0, blockSize-1)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	cr := resp.Header.Get("Content-Range")
	i := strings.LastIndexByte(cr, '/')
	if i < 0 {
		return fmt.Errorf("%s: missing Content-Range", r.url)
	}
	if r.size, err = strconv.ParseInt(cr[i+1:], 10, 64); err != nil {
		return fmt.Errorf("%s: bad Content-Range %q", r.url, cr)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, blockSize))
	if err != nil {
		return err
	}
	r.blocks[0] = b
	return nil
}

func (r *rangeReader) block(idx int64) ([]byte, error) {
	if b, ok := r.blocks[idx]; ok {
		return b, nil
	}
	from := idx * blockSize
	to := min(from+blockSize, r.size) - 1
	resp, err := r.get(from, to)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, blockSize))
	if err != nil {
		return nil, err
	}
	r.blocks[idx] = b
	return b, nil
}

func (r *rangeReader) Read(p []byte) (int, error) {
	if r.pos >= r.size {
		return 0, io.EOF
	}
	b, err := r.block(r.pos / blockSize)
	if err != nil {
		return 0, err
	}
	off := r.pos % blockSize
	if off >= int64(len(b)) {
		return 0, io.ErrUnexpectedEOF
	}
	n := copy(p, b[off:])
	r.pos += int64(n)
	return n, nil
}

func (r *rangeReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		r.pos = offset
	case io.SeekCurrent:
		r.pos += offset
	case io.SeekEnd:
		r.pos = r.size + offset
	}
	if r.pos < 0 {
		return 0, fmt.Errorf("negative seek")
	}
	return r.pos, nil
}
