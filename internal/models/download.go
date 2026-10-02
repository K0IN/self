package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ai-server/internal/errs"
)

// Downloader fetches files over HTTP with resume support.
type Downloader struct {
	Client *http.Client
	// BaseURL is the Hugging Face endpoint (default https://huggingface.co).
	BaseURL string
	// Token is an optional Hugging Face access token.
	Token string
	// Retries is how many times a failed transfer is retried (resuming from
	// the .part file) before giving up.
	Retries int
	// RetryBase is the first backoff delay; it doubles per attempt (max 30s).
	RetryBase time.Duration
	// StallTimeout aborts an attempt when no body bytes arrive for this long
	// (0 disables).
	StallTimeout time.Duration
}

// permanentErr marks errors that retrying cannot fix (404, auth).
type permanentErr struct{ error }

func (p permanentErr) Unwrap() error { return p.error }

func permanent(err error) error { return permanentErr{err} }

// NewDownloader returns a downloader honoring HF_ENDPOINT and HF_TOKEN.
func NewDownloader() *Downloader {
	base := os.Getenv("HF_ENDPOINT")
	if base == "" {
		base = "https://huggingface.co"
	}
	return &Downloader{
		Client: &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: 60 * time.Second,
			TLSHandshakeTimeout:   20 * time.Second,
		}},
		BaseURL:      strings.TrimRight(base, "/"),
		Token:        os.Getenv("HF_TOKEN"),
		Retries:      8,
		RetryBase:    2 * time.Second,
		StallTimeout: 60 * time.Second,
	}
}

// FileURL returns the resolve URL of a repo file on the requested revision.
func (d *Downloader) FileURL(repo, revision, file string) string {
	if revision == "" {
		revision = "main"
	}
	parts := strings.Split(file, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return fmt.Sprintf("%s/%s/resolve/%s/%s", d.BaseURL, repo, url.PathEscape(revision), strings.Join(parts, "/"))
}

// Progress receives download progress. total is -1 when unknown.
type Progress interface {
	Start(name string, done, total int64)
	Update(done int64)
	Finish(err error)
}

// Download fetches url to dest atomically: bytes are streamed to dest.part,
// which is renamed to dest only after the full body was received. An
// existing .part is resumed with an HTTP Range request when the server
// supports it. On cancellation the .part is kept for a later resume.
//
// Transient failures (connection resets, TLS errors, stalls, 5xx) are
// retried up to d.Retries times with exponential backoff, each retry
// resuming from the bytes already on disk.
//
// Only one process may download a given file at a time (dest.lock). When
// the server publishes the file's size and sha256 (Hugging Face
// X-Linked-Size / X-Linked-Etag), the result is verified before it is
// installed; a mismatching file is deleted, never installed.
func (d *Downloader) Download(ctx context.Context, rawURL, dest string, p Progress) error {
	return d.DownloadPinned(ctx, rawURL, dest, Pin{}, p)
}

// Pin is the expected size and sha256 of a file, from the registry. Zero
// values mean unknown.
type Pin struct {
	Size   int64
	SHA256 string
}

// DownloadPinned is Download with a known expected size and sha256. The
// pin always wins: if the server advertises a different size or hash the
// download is refused before any bytes are transferred (the upstream file
// changed and the registry must be updated), and the finished file must
// match the pin to be installed.
func (d *Downloader) DownloadPinned(ctx context.Context, rawURL, dest string, pin Pin, p Progress) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return errs.Wrap(errs.DownloadFailed, err, "cannot create model directory")
	}
	release, err := lockFile(dest + ".lock")
	if errors.Is(err, errLocked) {
		return errs.New(errs.DownloadFailed, "another self process is already downloading %s; wait for it to finish or stop it", filepath.Base(dest))
	}
	if err != nil {
		return errs.Wrap(errs.DownloadFailed, err, "cannot lock %s", dest)
	}
	defer release()
	// Another process may have finished while we waited for the lock.
	if st, err := os.Stat(dest); err == nil && st.Mode().IsRegular() && st.Size() > 0 && (pin.Size <= 0 || st.Size() == pin.Size) {
		return nil
	}
	want := d.expected(ctx, rawURL)
	if pin.Size > 0 || pin.SHA256 != "" {
		pinned := expectation{size: -1, sha256: strings.ToLower(pin.SHA256)}
		if pin.Size > 0 {
			pinned.size = pin.Size
		}
		if (want.size >= 0 && pinned.size >= 0 && want.size != pinned.size) ||
			(want.sha256 != "" && pinned.sha256 != "" && want.sha256 != pinned.sha256) {
			return errs.New(errs.DownloadFailed,
				"upstream file %s changed: server has %d bytes sha256 %s, registry pins %d bytes sha256 %s; update models/registry.yml",
				filepath.Base(dest), want.size, orUnknown(want.sha256), pinned.size, orUnknown(pinned.sha256))
		}
		want = pinned
	}

	for attempt := 0; ; attempt++ {
		err := d.downloadOnce(ctx, rawURL, dest, p, want)
		if err == nil {
			return nil
		}
		var pe permanentErr
		if errors.As(err, &pe) {
			return pe.error
		}
		if ctx.Err() != nil || attempt >= d.Retries {
			return err
		}
		wait := d.RetryBase << attempt
		if wait <= 0 || wait > 30*time.Second {
			wait = 30 * time.Second
		}
		if r, ok := p.(interface {
			Retry(err error, attempt, max int, wait time.Duration)
		}); ok {
			r.Retry(err, attempt+1, d.Retries, wait)
		}
		t := time.NewTimer(wait)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return errs.Wrap(errs.DownloadFailed, ctx.Err(), "download interrupted; progress kept in %s", dest+".part")
		}
	}
}

// expectation is what the server says the finished file must look like.
type expectation struct {
	size   int64  // -1 unknown
	sha256 string // lowercase hex, "" unknown
}

// expected asks the server (HEAD, no redirects) for the file's size and
// sha256. Hugging Face exposes them as X-Linked-Size / X-Linked-Etag on the
// resolve endpoint for LFS files. Failures are ignored (best effort).
func (d *Downloader) expected(ctx context.Context, rawURL string) expectation {
	e := expectation{size: -1}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return e
	}
	req.Header.Set("User-Agent", "self-ai-server/1")
	if d.Token != "" {
		req.Header.Set("Authorization", "Bearer "+d.Token)
	}
	client := *d.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Timeout = 30 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		return e
	}
	resp.Body.Close()
	if v, err := strconv.ParseInt(resp.Header.Get("X-Linked-Size"), 10, 64); err == nil && v > 0 {
		e.size = v
	}
	if h := strings.Trim(strings.TrimPrefix(resp.Header.Get("X-Linked-Etag"), "W/"), `"`); isSHA256(h) {
		e.sha256 = strings.ToLower(h)
	}
	return e
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func isSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// verify checks a completed .part against the expectation.
func verify(part string, want expectation) error {
	st, err := os.Stat(part)
	if err != nil {
		return err
	}
	if want.size >= 0 && st.Size() != want.size {
		return fmt.Errorf("size is %d bytes, expected %d", st.Size(), want.size)
	}
	if want.sha256 == "" {
		return nil
	}
	f, err := os.Open(part)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want.sha256 {
		return fmt.Errorf("sha256 is %s, expected %s", got, want.sha256)
	}
	return nil
}

func (d *Downloader) downloadOnce(ctx context.Context, rawURL, dest string, p Progress, want expectation) error {
	part := dest + ".part"
	var offset int64
	if st, err := os.Stat(part); err == nil && st.Mode().IsRegular() {
		offset = st.Size()
	}

	// Per-attempt context so a stalled transfer can be aborted and retried
	// without cancelling the whole download.
	attemptCtx, cancelAttempt := context.WithCancel(ctx)
	defer cancelAttempt()
	touch := func() {}
	if d.StallTimeout > 0 {
		stall := time.AfterFunc(d.StallTimeout, cancelAttempt)
		defer stall.Stop()
		touch = func() { stall.Reset(d.StallTimeout) }
	}

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return permanent(errs.Wrap(errs.DownloadFailed, err, "bad download URL"))
	}
	req.Header.Set("User-Agent", "self-ai-server/1")
	if d.Token != "" {
		req.Header.Set("Authorization", "Bearer "+d.Token)
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := d.Client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return errs.Wrap(errs.DownloadFailed, ctx.Err(), "download interrupted")
		}
		return errs.Wrap(errs.DownloadFailed, err, "download request failed")
	}
	defer resp.Body.Close()

	var total int64 = -1
	flags := os.O_CREATE | os.O_WRONLY
	switch resp.StatusCode {
	case http.StatusOK:
		offset = 0 // server ignored Range: restart
		flags |= os.O_TRUNC
		if resp.ContentLength >= 0 {
			total = resp.ContentLength
		}
	case http.StatusPartialContent:
		start, size, ok := parseContentRange(resp.Header.Get("Content-Range"))
		if !ok || start != offset {
			os.Remove(part) // restart from scratch on the next attempt
			return errs.New(errs.DownloadFailed, "server returned an unexpected range (%q)", resp.Header.Get("Content-Range"))
		}
		flags |= os.O_APPEND
		total = size
	case http.StatusRequestedRangeNotSatisfiable:
		// .part is at least as large as the file: verify via size and finish.
		_, size, ok := parseContentRange(resp.Header.Get("Content-Range"))
		if ok && size == offset {
			return finalize(part, dest, want)
		}
		os.Remove(part)
		return errs.New(errs.DownloadFailed, "partial download was invalid and has been removed; please retry")
	case http.StatusUnauthorized, http.StatusForbidden:
		return permanent(errs.New(errs.DownloadFailed, "access denied (HTTP %d); the repository may be gated — set HF_TOKEN", resp.StatusCode))
	case http.StatusNotFound:
		return permanent(errs.New(errs.DownloadFailed, "file not found (HTTP 404): %s", rawURL))
	default:
		return errs.New(errs.DownloadFailed, "unexpected HTTP status %d for %s", resp.StatusCode, rawURL)
	}

	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return errs.Wrap(errs.DownloadFailed, err, "cannot write %s", part)
	}
	if p != nil {
		p.Start(filepath.Base(dest), offset, total)
	}
	n, copyErr := io.Copy(f, &progressReader{r: resp.Body, p: p, done: offset, onRead: touch})
	syncErr := f.Sync()
	closeErr := f.Close()
	got := offset + n
	switch {
	case copyErr != nil:
		err = copyErr
		if ctx.Err() != nil {
			err = ctx.Err()
		} else if attemptCtx.Err() != nil {
			err = fmt.Errorf("no data received for %s", d.StallTimeout)
		}
	case syncErr != nil:
		err = syncErr
	case closeErr != nil:
		err = closeErr
	case total >= 0 && got != total:
		err = fmt.Errorf("received %d of %d bytes", got, total)
	}
	if p != nil {
		p.Finish(err)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return errs.Wrap(errs.DownloadFailed, err, "download interrupted; progress kept in %s", part)
		}
		return errs.Wrap(errs.DownloadFailed, err, "download failed; progress kept in %s", part)
	}
	return finalize(part, dest, want)
}

// finalize verifies part and atomically installs it as dest. A corrupt part
// is deleted so the next attempt starts clean.
func finalize(part, dest string, want expectation) error {
	if err := verify(part, want); err != nil {
		os.Remove(part)
		return errs.Wrap(errs.DownloadFailed, err, "downloaded file failed verification and was deleted")
	}
	if err := os.Rename(part, dest); err != nil {
		return errs.Wrap(errs.DownloadFailed, err, "cannot move download into place")
	}
	return nil
}

// parseContentRange parses "bytes start-end/size".
func parseContentRange(h string) (start, size int64, ok bool) {
	h = strings.TrimSpace(h)
	if !strings.HasPrefix(h, "bytes ") {
		return 0, 0, false
	}
	rng, sz, found := strings.Cut(h[6:], "/")
	if !found {
		return 0, 0, false
	}
	size, err := strconv.ParseInt(sz, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	if rng == "*" {
		return 0, size, true
	}
	s, _, found := strings.Cut(rng, "-")
	if !found {
		return 0, 0, false
	}
	start, err = strconv.ParseInt(s, 10, 64)
	return start, size, err == nil
}

type progressReader struct {
	r      io.Reader
	p      Progress
	done   int64
	onRead func()
}

func (pr *progressReader) Read(b []byte) (int, error) {
	n, err := pr.r.Read(b)
	if n > 0 {
		if pr.onRead != nil {
			pr.onRead()
		}
		pr.done += int64(n)
		if pr.p != nil {
			pr.p.Update(pr.done)
		}
	}
	return n, err
}
