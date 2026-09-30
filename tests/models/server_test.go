package models_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// syncBuffer collects the server's stderr while it is still being written.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// tail returns the last n non-empty lines.
func (b *syncBuffer) tail(n int) string {
	var lines []string
	for _, l := range strings.Split(b.String(), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// serveProc is one `self serve` child process.
type serveProc struct {
	cmd     *exec.Cmd
	base    string
	log     *syncBuffer
	exited  chan struct{}
	waitErr error // valid once exited is closed
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// startServe launches `bin serve ...` on a free port. mirror, when non-nil,
// also receives the server's stderr.
func startServe(bin string, args []string, mirror io.Writer) (*serveProc, error) {
	port, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("no free port: %w", err)
	}
	p := &serveProc{base: fmt.Sprintf("http://127.0.0.1:%d", port), log: &syncBuffer{}, exited: make(chan struct{})}
	var w io.Writer = p.log
	if mirror != nil {
		w = io.MultiWriter(p.log, mirror)
	}
	p.cmd = exec.Command(bin, append(args, "--port", fmt.Sprint(port))...)
	p.cmd.Stderr = w
	if err := p.cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		p.waitErr = p.cmd.Wait()
		close(p.exited)
	}()
	return p, nil
}

func (p *serveProc) waitReady(ctx context.Context, timeout time.Duration) error {
	probe := &http.Client{Timeout: 2 * time.Second}
	deadline := time.After(timeout)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		if resp, err := probe.Get(p.base + "/health"); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-p.exited:
			return fmt.Errorf("self serve exited before the API was ready (%v)\n%s", p.waitErr, p.log.tail(15))
		case <-deadline:
			return fmt.Errorf("API not ready after %s\n%s", timeout, p.log.tail(15))
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// stop asks the server to shut down and reports whether it did so cleanly:
// exit status 0 within grace after an interrupt.
func (p *serveProc) stop(grace time.Duration) error {
	select {
	case <-p.exited:
		return fmt.Errorf("self serve had already exited: %v\n%s", p.waitErr, p.log.tail(15))
	default:
	}
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		_ = p.cmd.Process.Kill() // no interrupt on this platform
	}
	select {
	case <-p.exited:
		if p.waitErr != nil {
			return fmt.Errorf("self serve exited with %v\n%s", p.waitErr, p.log.tail(15))
		}
		return nil
	case <-time.After(grace):
		_ = p.cmd.Process.Kill()
		<-p.exited
		return fmt.Errorf("self serve still running %s after interrupt; killed", grace)
	}
}
