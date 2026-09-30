package runtime

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"ai-server/internal/errs"
)

// The test binary doubles as a fake engine: it hangs, or exits at once.
func TestMain(m *testing.M) {
	switch os.Getenv("RUNTIME_TEST_ENGINE") {
	case "hang":
		time.Sleep(time.Minute)
		return
	case "exit":
		os.Exit(3)
	}
	os.Exit(m.Run())
}

func startFake(t *testing.T, mode string) *Supervisor {
	t.Helper()
	p, err := Start(Spec{Path: os.Args[0], Env: []string{"RUNTIME_TEST_ENGINE=" + mode}})
	if err != nil {
		t.Fatal(err)
	}
	s := Supervise(p, "fake engine")
	t.Cleanup(s.Kill)
	return s
}

// readStdout blocks until the engine writes something or dies.
func readStdout(s *Supervisor) error {
	_, err := s.Stdout().Read(make([]byte, 1))
	if err == nil || err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}

func TestAwaitKillsHungEngine(t *testing.T) {
	s := startFake(t, "hang")
	err := s.Await(100*time.Millisecond, func() error { return readStdout(s) })
	if errs.KindOf(err) != errs.RuntimeCrashed || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("err = %v", err)
	}
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("engine still running after the watchdog fired")
	}
}

func TestAwaitReportsCrash(t *testing.T) {
	s := startFake(t, "exit")
	err := s.Await(time.Minute, func() error { return readStdout(s) })
	if errs.KindOf(err) != errs.RuntimeCrashed || !strings.Contains(err.Error(), "fake engine") {
		t.Fatalf("err = %v", err)
	}
}

func TestHandshakeFailureAndCancel(t *testing.T) {
	s := startFake(t, "exit")
	err := Handshake(context.Background(), s, func() error { return readStdout(s) })
	if errs.KindOf(err) != errs.RuntimeStartFailed || !strings.Contains(err.Error(), "failed to start") {
		t.Fatalf("exit: err = %v", err)
	}

	s = startFake(t, "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = Handshake(ctx, s, func() error { return readStdout(s) })
	if errs.KindOf(err) != errs.RuntimeStartFailed || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("cancel: err = %v", err)
	}
}
