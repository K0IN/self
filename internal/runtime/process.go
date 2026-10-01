// Package runtime starts and supervises native engine subprocesses and
// locates bundled engine executables. It is protocol-agnostic: adapters own
// what is written to stdin and read from stdout.
package runtime

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"
)

// Spec describes an engine process.
type Spec struct {
	Path string
	Args []string
	// Env entries are appended to the parent environment.
	Env []string
	// LibDir, when set, is prepended to the platform's shared library
	// search path for the child only.
	LibDir string
	// Log receives stderr lines as they arrive (nil = only keep a tail).
	Log io.Writer
	// LogPrefix is prepended to forwarded stderr lines.
	LogPrefix string
}

// Process is a running engine.
type Process struct {
	cmd    *exec.Cmd
	stdin  *os.File
	stdout *os.File

	done    chan struct{}
	waitErr error

	stderrDone chan struct{}
	tailMu     sync.Mutex
	tail       []string

	stopOnce sync.Once
}

const tailLines = 200

// Start launches the engine. The child gets its own process group (Unix) so
// terminal signals reach only the parent, which then shuts the child down in
// order. On Linux the child is also killed if the parent dies.
func Start(spec Spec) (*Process, error) {
	if spec.Log != nil {
		fmt.Fprintf(spec.Log, "%sexec %s\n", spec.LogPrefix, FormatCommand(spec.Path, spec.Args))
	}
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Env = append(os.Environ(), spec.Env...)
	if spec.LibDir != "" {
		cmd.Env = withLibDir(cmd.Env, spec.LibDir)
	}
	cmd.Dir = filepath.Dir(spec.Path)
	setSysProcAttr(cmd)

	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		return nil, err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		outR.Close()
		outW.Close()
		return nil, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, errW
	if err := cmd.Start(); err != nil {
		for _, f := range []*os.File{inR, inW, outR, outW, errR, errW} {
			f.Close()
		}
		return nil, err
	}
	// Close the child's ends in the parent so EOF propagates.
	inR.Close()
	outW.Close()
	errW.Close()

	p := &Process{
		cmd:        cmd,
		stdin:      inW,
		stdout:     outR,
		done:       make(chan struct{}),
		stderrDone: make(chan struct{}),
	}
	go p.captureStderr(errR, spec.Log, spec.LogPrefix)
	go func() {
		err := cmd.Wait()
		<-p.stderrDone // make the full stderr tail available before Done
		p.waitErr = err
		close(p.done)
	}()
	return p, nil
}

func (p *Process) captureStderr(r *os.File, log io.Writer, prefix string) {
	defer close(p.stderrDone)
	defer r.Close()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		p.tailMu.Lock()
		p.tail = append(p.tail, line)
		if len(p.tail) > tailLines {
			p.tail = p.tail[len(p.tail)-tailLines:]
		}
		p.tailMu.Unlock()
		if log != nil {
			fmt.Fprintf(log, "%s%s\n", prefix, line)
		}
	}
	// Drain anything left (overlong line) so the child never blocks.
	io.Copy(io.Discard, r)
}

// Stdin is the child's stdin (protocol input).
func (p *Process) Stdin() io.Writer { return p.stdin }

// Stdout is the child's stdout (protocol output only).
func (p *Process) Stdout() io.Reader { return p.stdout }

// Done is closed after the child has exited and stderr is drained.
func (p *Process) Done() <-chan struct{} { return p.done }

// ExitError returns the wait error after Done is closed.
func (p *Process) ExitError() error {
	select {
	case <-p.done:
		return p.waitErr
	default:
		return nil
	}
}

// ExitDescription is a human-readable exit status.
func (p *Process) ExitDescription() string {
	err := p.ExitError()
	if err == nil {
		return "exited with status 0"
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return describeExit(ee)
	}
	return err.Error()
}

// StderrTail returns up to n most recent stderr lines.
func (p *Process) StderrTail(n int) []string {
	p.tailMu.Lock()
	defer p.tailMu.Unlock()
	if n > len(p.tail) {
		n = len(p.tail)
	}
	return append([]string(nil), p.tail[len(p.tail)-n:]...)
}

// Stop shuts the child down: close stdin and wait up to grace, then
// SIGTERM and wait up to 3s, then SIGKILL. It always reaps the child.
func (p *Process) Stop(ctx context.Context, grace time.Duration) error {
	p.stopOnce.Do(func() { p.stdin.Close() })
	wait := func(d time.Duration) bool {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-p.done:
			return true
		case <-t.C:
			return false
		case <-ctx.Done():
			return false
		}
	}
	if wait(grace) {
		return p.finish()
	}
	terminate(p.cmd)
	if wait(3 * time.Second) {
		return p.finish()
	}
	kill(p.cmd)
	<-p.done
	return p.finish()
}

// Kill terminates the child immediately and reaps it.
func (p *Process) Kill() {
	p.stopOnce.Do(func() { p.stdin.Close() })
	kill(p.cmd)
	<-p.done
	p.stdout.Close()
}

func (p *Process) finish() error {
	p.stdout.Close()
	return nil
}

// FormatTail joins stderr tail lines for error messages.
func FormatTail(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return "\n  engine stderr:\n    " + strings.Join(lines, "\n    ")
}

// FormatCommand renders path and args as one shell-quoted line for logs.
func FormatCommand(path string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	for _, a := range append([]string{path}, args...) {
		if a != "" && strings.Trim(a, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./:=,@%+-") == "" {
			parts = append(parts, a)
			continue
		}
		parts = append(parts, "'"+strings.ReplaceAll(a, "'", `'\''`)+"'")
	}
	return strings.Join(parts, " ")
}

func withLibDir(env []string, dir string) []string {
	key := "LD_LIBRARY_PATH"
	switch goruntime.GOOS {
	case "darwin":
		key = "DYLD_LIBRARY_PATH"
	case "windows":
		key = "PATH"
	}
	for i, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if ok && strings.EqualFold(k, key) {
			if v != "" {
				v = string(os.PathListSeparator) + v
			}
			env[i] = k + "=" + dir + v
			return env
		}
	}
	return append(env, key+"="+dir)
}
