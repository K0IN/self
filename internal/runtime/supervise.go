package runtime

import (
	"context"
	"fmt"
	"time"

	"ai-server/internal/errs"
)

// stopGrace is how long Close waits for the engine to exit after its stdin
// closes, before it is terminated.
const stopGrace = 5 * time.Second

// Supervisor adds the failure reporting every adapter needs to a Process:
// start and crash errors that carry the exit status and stderr tail, a
// watchdog around requests, and a graceful Close.
type Supervisor struct {
	*Process
	label string
}

// Supervise wraps a started process. label names the engine in errors,
// e.g. "decision engine".
func Supervise(p *Process, label string) *Supervisor { return &Supervisor{Process: p, label: label} }

// Handshake runs read, which must consume the engine's first message. The
// engine is killed and a start error returned if read fails or ctx ends first.
func Handshake(ctx context.Context, s *Supervisor, read func() error) error {
	ch := make(chan error, 1)
	go func() { ch <- read() }()
	select {
	case err := <-ch:
		if err != nil {
			s.Kill()
			return errs.Wrap(errs.RuntimeStartFailed, err, "engine failed to start (%s)%s",
				s.ExitDescription(), FormatTail(s.StderrTail(15)))
		}
		return nil
	case <-ctx.Done():
		s.Kill()
		return errs.Wrap(errs.RuntimeStartFailed, ctx.Err(), "engine start cancelled")
	}
}

// Await runs read, which must consume the engine's answer to a request that
// was just sent. A read that outlasts timeout kills the engine.
func (s *Supervisor) Await(timeout time.Duration, read func() error) error {
	watchdog := time.AfterFunc(timeout, s.Kill)
	err := read()
	inTime := watchdog.Stop()
	if err == nil {
		return nil
	}
	if !inTime {
		return errs.New(errs.RuntimeCrashed, "engine did not answer within %s and was stopped", timeout)
	}
	return s.CrashErr(err)
}

// CrashErr reports that the engine died or its stream broke. cause may be nil.
func (s *Supervisor) CrashErr(cause error) error {
	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		// The stream broke but the process lingers; make sure it is gone.
		s.Kill()
	}
	return &errs.Error{
		Kind:    errs.RuntimeCrashed,
		Message: fmt.Sprintf("%s %s%s", s.label, s.ExitDescription(), FormatTail(s.StderrTail(15))),
		Err:     cause,
	}
}

// Close stops the engine: stdin EOF, then SIGTERM, then SIGKILL. It is safe
// on a nil Supervisor (adapter never started).
func (s *Supervisor) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return s.Stop(ctx, stopGrace)
}

// StderrTail returns recent engine stderr for crash reports. Safe on nil.
func (s *Supervisor) StderrTail(n int) []string {
	if s == nil {
		return nil
	}
	return s.Process.StderrTail(n)
}
