package models

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ai-server/internal/errs"
	"ai-server/internal/registry"
)

type recProgress struct {
	mu                     sync.Mutex
	startDone, total, last int64
	finished               bool
	err                    error
	retries                int
}

func (r *recProgress) Start(_ string, done, total int64) {
	r.mu.Lock()
	r.startDone, r.total = done, total
	r.mu.Unlock()
}
func (r *recProgress) Update(done int64) { r.mu.Lock(); r.last = done; r.mu.Unlock() }
func (r *recProgress) Finish(err error)  { r.mu.Lock(); r.finished, r.err = true, err; r.mu.Unlock() }
func (r *recProgress) Retry(error, int, int, time.Duration) {
	r.mu.Lock()
	r.retries++
	r.mu.Unlock()
}

func payload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

// newDL returns a downloader tuned for fast tests.
func newDL() *Downloader {
	d := NewDownloader()
	d.RetryBase = time.Millisecond
	d.StallTimeout = 2 * time.Second
	return d
}

// serveGET wraps h so HEAD probes (size/hash lookup) get an empty 200.
func serveGET(h http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			return
		}
		h(w, r)
	}))
}

func TestDownloadAtomicWithProgress(t *testing.T) {
	data := payload(100_000)
	srv := serveGET(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "m.gguf", time.Time{}, bytes.NewReader(data))
	})
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "a", "m.gguf")
	p := &recProgress{}
	if err := newDL().Download(context.Background(), srv.URL, dest, p); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatal("content mismatch")
	}
	for _, s := range []string{".part", ".lock"} {
		if _, err := os.Stat(dest + s); !os.IsNotExist(err) {
			t.Fatalf("%s left behind", s)
		}
	}
	if p.total != int64(len(data)) || p.last != int64(len(data)) || !p.finished || p.err != nil {
		t.Fatalf("progress = %+v", p)
	}
}

func TestDownloadResume(t *testing.T) {
	data := payload(50_000)
	var sawRange atomic.Value
	srv := serveGET(func(w http.ResponseWriter, r *http.Request) {
		sawRange.Store(r.Header.Get("Range"))
		http.ServeContent(w, r, "m.gguf", time.Time{}, bytes.NewReader(data))
	})
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "m.gguf")
	os.WriteFile(dest+".part", data[:20_000], 0o644)
	p := &recProgress{}
	if err := newDL().Download(context.Background(), srv.URL, dest, p); err != nil {
		t.Fatal(err)
	}
	if sawRange.Load() != "bytes=20000-" {
		t.Fatalf("range = %v", sawRange.Load())
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) || p.startDone != 20_000 || p.total != 50_000 {
		t.Fatalf("resume failed: len=%d progress=%+v", len(got), p)
	}
}

func TestDownloadServerIgnoresRange(t *testing.T) {
	data := payload(10_000)
	srv := serveGET(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.Write(data) // always 200
	})
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "m.gguf")
	os.WriteFile(dest+".part", []byte("garbage-prefix"), 0o644)
	if err := newDL().Download(context.Background(), srv.URL, dest, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); !bytes.Equal(got, data) {
		t.Fatal("restart did not truncate")
	}
}

func dropAfter(w http.ResponseWriter, b []byte) {
	w.Write(b)
	w.(http.Flusher).Flush()
	if c, _, err := w.(http.Hijacker).Hijack(); err == nil {
		c.Close()
	}
}

func TestDownloadInterruptedKeepsPart(t *testing.T) {
	data := payload(200_000)
	srv := serveGET(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		dropAfter(w, data[:60_000])
	})
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "m.gguf")
	d := newDL()
	d.Retries = 0
	err := d.Download(context.Background(), srv.URL, dest, nil)
	if errs.KindOf(err) != errs.DownloadFailed {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("partial file installed as model")
	}
	st, err := os.Stat(dest + ".part")
	if err != nil || st.Size() != 60_000 {
		t.Fatalf("part not preserved: %v", err)
	}
}

func TestDownloadRetriesAndResumes(t *testing.T) {
	data := payload(120_000)
	var mu sync.Mutex
	var ranges []string
	srv := serveGET(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		n := len(ranges)
		mu.Unlock()
		if n > 2 {
			http.ServeContent(w, r, "m.gguf", time.Time{}, bytes.NewReader(data))
			return
		}
		start := 0
		if rg := r.Header.Get("Range"); rg != "" {
			fmt.Sscanf(rg, "bytes=%d-", &start)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
			w.Header().Set("Content-Length", fmt.Sprint(len(data)-start))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		}
		dropAfter(w, data[start:start+30_000])
	})
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "m.gguf")
	p := &recProgress{}
	if err := newDL().Download(context.Background(), srv.URL, dest, p); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); !bytes.Equal(got, data) {
		t.Fatal("content mismatch after retries")
	}
	if p.retries != 2 || len(ranges) != 3 || ranges[1] != "bytes=30000-" || ranges[2] != "bytes=60000-" {
		t.Fatalf("retries=%d ranges=%v", p.retries, ranges)
	}
}

func TestDownloadStallRetried(t *testing.T) {
	data := payload(10_000)
	var calls int32
	stop := make(chan struct{})
	defer close(stop)
	srv := serveGET(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			w.Write(data[:100])
			w.(http.Flusher).Flush()
			select { // stall until the client gives up
			case <-r.Context().Done():
			case <-stop:
			}
			return
		}
		http.ServeContent(w, r, "m.gguf", time.Time{}, bytes.NewReader(data))
	})
	defer srv.Close()
	d := newDL()
	d.StallTimeout = 200 * time.Millisecond
	dest := filepath.Join(t.TempDir(), "m.gguf")
	if err := d.Download(context.Background(), srv.URL, dest, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); !bytes.Equal(got, data) {
		t.Fatal("content mismatch after stall")
	}
}

func TestDownloadVerifiesSHA256AndSize(t *testing.T) {
	data := payload(5000)
	sum := sha256.Sum256(data)
	var corrupt atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("X-Linked-Size", fmt.Sprint(len(data)))
			w.Header().Set("X-Linked-Etag", `"`+hex.EncodeToString(sum[:])+`"`)
			return
		}
		body := data
		if corrupt.Load() {
			body = append([]byte{}, data...)
			body[10] ^= 0xff
		}
		w.Write(body)
	}))
	defer srv.Close()
	dir := t.TempDir()
	d := newDL()
	d.Retries = 0
	if err := d.Download(context.Background(), srv.URL, filepath.Join(dir, "ok.gguf"), nil); err != nil {
		t.Fatal(err)
	}
	corrupt.Store(true)
	bad := filepath.Join(dir, "bad.gguf")
	err := d.Download(context.Background(), srv.URL, bad, nil)
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("corrupt file accepted: %v", err)
	}
	for _, p := range []string{bad, bad + ".part"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s left behind", p)
		}
	}
}

func TestDownloadLockPreventsConcurrentWriters(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "m.gguf")
	release, err := lockFile(dest + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	err = newDL().Download(context.Background(), "http://127.0.0.1:1/never", dest, nil)
	if err == nil || !strings.Contains(err.Error(), "already downloading") {
		t.Fatalf("got %v", err)
	}
}

func TestDownloadCancelled(t *testing.T) {
	srv := serveGET(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000000")
		w.Write(make([]byte, 1000))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	dest := filepath.Join(t.TempDir(), "m.gguf")
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	err := newDL().Download(ctx, srv.URL, dest, nil)
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(dest + ".part"); err != nil {
		t.Fatal("part removed on cancel")
	}
}

func TestDownloadHTTPErrors(t *testing.T) {
	srv := serveGET(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "gated") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.NotFound(w, r)
	})
	defer srv.Close()
	dir := t.TempDir()
	for _, p := range []string{"/missing", "/gated"} {
		// permanent errors must not be retried
		start := time.Now()
		err := newDL().Download(context.Background(), srv.URL+p, filepath.Join(dir, "x.gguf"), nil)
		if errs.KindOf(err) != errs.DownloadFailed || time.Since(start) > time.Second {
			t.Errorf("%s: %v (%s)", p, err, time.Since(start))
		}
	}
}

func TestStoreLayoutAndEnsure(t *testing.T) {
	data := payload(1000)
	var hits int32
	srv := serveGET(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.URL.Path != "/mys/kev-4b-GGUF/resolve/main/kev.gguf" {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	})
	defer srv.Close()
	res := pinnedModel(t, int64(len(data)), sha(data))
	s := Store{Root: t.TempDir()}
	d := newDL()
	d.BaseURL = srv.URL
	files, err := Ensure(context.Background(), s, d, res, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(s.Root, "kev", "4b", "q4", "kev.gguf")
	if files[registry.RoleModel] != want {
		t.Fatalf("path = %s", files[registry.RoleModel])
	}
	if _, err := Ensure(context.Background(), s, d, res, nil); err != nil || hits != 1 {
		t.Fatalf("second ensure re-downloaded: hits=%d err=%v", hits, err)
	}
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// pinnedModel builds a one-file registry entry pinned to size/sum.
func pinnedModel(t *testing.T, size int64, sum string) registry.Resolved {
	t.Helper()
	doc := fmt.Sprintf(`version: 1
models:
  kev:4b:
    description: Kev
    readme: readmes/kev/4b.md
    type: decision
    capabilities:
      input: [text]
      output: [noul]
    q4:
      adapter: ggmlc-laya
      repo: mys/kev-4b-GGUF
      files:
        - {file: kev.gguf, size: %d, sha256: %s}
`, size, sum)
	reg, err := registry.Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	res, err := reg.Resolve("kev:4b", registry.ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// The registry pin is authoritative: a file whose bytes differ from the
// pinned sha256 is deleted and never installed, even if the server does not
// publish any hash itself.
func TestEnsureRejectsPinMismatch(t *testing.T) {
	data := payload(500)
	srv := serveGET(func(w http.ResponseWriter, r *http.Request) { w.Write(data) })
	defer srv.Close()
	d := newDL()
	d.BaseURL = srv.URL
	d.Retries = 0
	s := Store{Root: t.TempDir()}
	res := pinnedModel(t, int64(len(data)), strings.Repeat("0", 64))
	_, err := Ensure(context.Background(), s, d, res, nil)
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("err = %v", err)
	}
	p := s.Path(res, res.Variant.Files[0])
	if _, err := os.Stat(p); err == nil {
		t.Fatal("mismatching file was installed")
	}
	if _, err := os.Stat(p + ".part"); err == nil {
		t.Fatal("mismatching .part kept")
	}
}

// If the server advertises a different file than the registry pins, the
// download is refused before transferring the body.
func TestEnsureRefusesChangedUpstream(t *testing.T) {
	data := payload(500)
	var gets int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Linked-Size", "999")
		w.Header().Set("X-Linked-Etag", `"`+strings.Repeat("1", 64)+`"`)
		if r.Method == http.MethodGet {
			atomic.AddInt32(&gets, 1)
			w.Write(data)
		}
	}))
	defer srv.Close()
	d := newDL()
	d.BaseURL = srv.URL
	s := Store{Root: t.TempDir()}
	res := pinnedModel(t, int64(len(data)), sha(data))
	_, err := Ensure(context.Background(), s, d, res, nil)
	if err == nil || !strings.Contains(err.Error(), "upstream file kev.gguf changed") {
		t.Fatalf("err = %v", err)
	}
	if gets != 0 {
		t.Fatalf("body downloaded %d times", gets)
	}
}

// An installed file with the wrong size (older upstream revision) is not
// treated as installed.
func TestInstalledChecksPinnedSize(t *testing.T) {
	s := Store{Root: t.TempDir()}
	res := pinnedModel(t, 10, strings.Repeat("a", 64))
	f := res.Variant.Files[0]
	p := s.Path(res, f)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("12345"), 0o644)
	if s.Installed(res, f) {
		t.Fatal("wrong-size file counted as installed")
	}
	os.WriteFile(p, []byte("1234567890"), 0o644)
	if !s.Installed(res, f) {
		t.Fatal("correct-size file not installed")
	}
}

func TestStoreRemove(t *testing.T) {
	s := Store{Root: t.TempDir()}
	put := func(name, tag, quant string) string {
		dir := filepath.Join(s.Root, name, tag, quant)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "m.gguf.part"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	q4, q8, other := put("kev", "4b", "q4"), put("kev", "4b", "q8"), put("kev", "0.5b", "q4")
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	got, err := s.Remove("kev:4b", "q8")
	if err != nil || len(got) != 1 || got[0] != "q8" || exists(q8) || !exists(q4) {
		t.Fatalf("remove q8: got=%v err=%v", got, err)
	}
	got, err = s.Remove("kev:4b", "")
	if err != nil || len(got) != 1 || got[0] != "q4" || exists(filepath.Join(s.Root, "kev", "4b")) {
		t.Fatalf("remove all: got=%v err=%v", got, err)
	}
	if !exists(other) {
		t.Fatal("removing kev:4b touched kev:0.5b")
	}
	if got, err = s.Remove("kev:4b", ""); err != nil || len(got) != 0 {
		t.Fatalf("remove missing: got=%v err=%v", got, err)
	}
	for _, bad := range [][2]string{{"../kev:4b", ""}, {"kev:..", ""}, {"kev:4b", "../q4"}, {"kev/x:4b", ""}, {":4b", ""}} {
		if _, err := s.Remove(bad[0], bad[1]); errs.KindOf(err) != errs.InvalidRequest {
			t.Fatalf("Remove(%q, %q) err = %v", bad[0], bad[1], err)
		}
	}
	if !exists(other) {
		t.Fatal("invalid reference deleted files")
	}
}

func TestProgressLines(t *testing.T) {
	now := time.Unix(0, 0)
	var buf bytes.Buffer
	p := &TerminalProgress{W: &buf, TTY: true, now: func() time.Time { return now }}
	p.Start("x.gguf", 0, 100<<20)
	now = now.Add(time.Second)
	p.Update(79 << 20)
	l := p.Lines()
	if l[0] != "79.00 MiB / 100 MiB" || !strings.HasSuffix(l[1], " 79%") || !strings.Contains(l[2], "79.00 MiB/s") || !strings.Contains(l[2], "ETA 0s") {
		t.Fatalf("lines = %q", l)
	}
	if FormatBytes(2_480_000_000) != "2.31 GiB" {
		t.Fatal(FormatBytes(2_480_000_000))
	}
}
