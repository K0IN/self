package decision

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ai-server/internal/errs"
)

func reqN(n int) Request { return Request{State: json.RawMessage(`"` + string(rune('a'+n)) + `"`)} }

func TestSchedulerSingleRunnerConcurrentCallers(t *testing.T) {
	var running, maxRunning, total int32
	s := NewScheduler(64, func(ctx context.Context, r Request) (Response, error) {
		n := atomic.AddInt32(&running, 1)
		for {
			m := atomic.LoadInt32(&maxRunning)
			if n <= m || atomic.CompareAndSwapInt32(&maxRunning, m, n) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		atomic.AddInt32(&running, -1)
		atomic.AddInt32(&total, 1)
		return Response{Usage: Usage{InputTokens: 1}}, nil
	})
	defer s.Close()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := s.Submit(context.Background(), reqN(i)); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if maxRunning != 1 || total != 20 {
		t.Fatalf("maxRunning=%d total=%d", maxRunning, total)
	}
}

// blockingRunner lets tests control when each run completes.
type blockingRunner struct {
	mu      sync.Mutex
	order   []string
	started chan string
	release chan struct{}
}

func newBlockingRunner() *blockingRunner {
	return &blockingRunner{started: make(chan string, 100), release: make(chan struct{}, 100)}
}

func (b *blockingRunner) run(ctx context.Context, r Request) (Response, error) {
	b.mu.Lock()
	b.order = append(b.order, string(r.State))
	b.mu.Unlock()
	b.started <- string(r.State)
	<-b.release
	return Response{}, nil
}

func TestSchedulerFIFO(t *testing.T) {
	b := newBlockingRunner()
	s := NewScheduler(8, b.run)
	defer s.Close()
	results := make(chan error, 4)
	go func() { _, err := s.Submit(context.Background(), reqN(0)); results <- err }()
	<-b.started // first is running; queue the rest in order
	for i := 1; i < 4; i++ {
		go func(i int) { _, err := s.Submit(context.Background(), reqN(i)); results <- err }(i)
		waitQueue(t, s, i)
	}
	for i := 0; i < 4; i++ {
		b.release <- struct{}{}
	}
	for i := 0; i < 4; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	want := []string{`"a"`, `"b"`, `"c"`, `"d"`}
	for i, w := range want {
		if b.order[i] != w {
			t.Fatalf("order = %v", b.order)
		}
	}
}

func waitQueue(t *testing.T, s *Scheduler, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for s.QueueLen() < n {
		if time.Now().After(deadline) {
			t.Fatalf("queue len %d, want %d", s.QueueLen(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSchedulerQueueFull(t *testing.T) {
	b := newBlockingRunner()
	s := NewScheduler(1, b.run)
	defer func() {
		for i := 0; i < 3; i++ {
			b.release <- struct{}{}
		}
		s.Close()
	}()
	go s.Submit(context.Background(), reqN(0))
	<-b.started
	go s.Submit(context.Background(), reqN(1))
	waitQueue(t, s, 1)
	_, err := s.Submit(context.Background(), reqN(2))
	if errs.KindOf(err) != errs.QueueFull {
		t.Fatalf("got %v", err)
	}
}

func TestSchedulerQueuedCancellationSkipsRun(t *testing.T) {
	b := newBlockingRunner()
	s := NewScheduler(4, b.run)
	defer s.Close()
	done0 := make(chan struct{})
	go func() { s.Submit(context.Background(), reqN(0)); close(done0) }()
	<-b.started
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := s.Submit(ctx, reqN(1)); errc <- err }()
	waitQueue(t, s, 1)
	cancel()
	if err := <-errc; errs.KindOf(err) != errs.Timeout {
		t.Fatalf("got %v", err)
	}
	b.release <- struct{}{}
	<-done0
	// Submit a third job and verify the cancelled one never ran.
	go s.Submit(context.Background(), reqN(2))
	if got := <-b.started; got != `"c"` {
		t.Fatalf("cancelled job ran: %s", got)
	}
	b.release <- struct{}{}
}

func TestSchedulerRunningCancellationCompletes(t *testing.T) {
	b := newBlockingRunner()
	s := NewScheduler(4, b.run)
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := s.Submit(ctx, reqN(0)); errc <- err }()
	<-b.started
	cancel()
	if err := <-errc; errs.KindOf(err) != errs.Timeout {
		t.Fatalf("got %v", err)
	}
	b.release <- struct{}{} // running pass completes; result is discarded
	// runner continues with the next request
	resc := make(chan error, 1)
	go func() { _, err := s.Submit(context.Background(), reqN(1)); resc <- err }()
	<-b.started
	b.release <- struct{}{}
	if err := <-resc; err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerCrashFailsQueued(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	s := NewScheduler(4, func(ctx context.Context, r Request) (Response, error) {
		started <- struct{}{}
		<-release
		return Response{}, errs.New(errs.RuntimeCrashed, "engine exited")
	})
	defer s.Close()
	errs1 := make(chan error, 2)
	go func() { _, err := s.Submit(context.Background(), reqN(0)); errs1 <- err }()
	<-started
	go func() { _, err := s.Submit(context.Background(), reqN(1)); errs1 <- err }()
	waitQueue(t, s, 1)
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-errs1; errs.KindOf(err) != errs.RuntimeCrashed {
			t.Fatalf("got %v", err)
		}
	}
	if _, err := s.Submit(context.Background(), reqN(2)); errs.KindOf(err) != errs.RuntimeCrashed {
		t.Fatalf("after crash: %v", err)
	}
}
