package decision

import (
	"context"
	"sync"
	"time"

	"ai-server/internal/errs"
)

// runFunc executes one request on the engine.
type runFunc func(ctx context.Context, req Request) (Response, error)

type job struct {
	ctx      context.Context
	req      Request
	enqueued time.Time
	result   chan jobResult // buffered(1)
}

type jobResult struct {
	resp Response
	err  error
}

// Scheduler feeds ready (preprocessed) requests to a single runner in FIFO
// order through a bounded queue.
//
// Cancellation semantics:
//   - a job whose context is done while queued is skipped (never run);
//   - a job that is already running completes; its result is discarded if
//     the caller has gone away. The engine is never killed for that.
type Scheduler struct {
	run   runFunc
	queue chan *job

	mu      sync.Mutex
	closed  bool
	failErr error // set when the runner is unusable

	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewScheduler creates a scheduler with the given queue capacity and starts
// its worker.
func NewScheduler(queueSize int, run runFunc) *Scheduler {
	if queueSize < 1 {
		queueSize = 1
	}
	s := &Scheduler{run: run, queue: make(chan *job, queueSize), stop: make(chan struct{})}
	s.wg.Add(1)
	go s.worker()
	return s
}

// Submit enqueues req and waits for its result or ctx cancellation.
func (s *Scheduler) Submit(ctx context.Context, req Request) (Response, error) {
	j := &job{ctx: ctx, req: req, enqueued: time.Now(), result: make(chan jobResult, 1)}
	s.mu.Lock()
	switch {
	case s.failErr != nil:
		err := s.failErr
		s.mu.Unlock()
		return Response{}, err
	case s.closed:
		s.mu.Unlock()
		return Response{}, errs.New(errs.ShuttingDown, "The server is shutting down.")
	}
	select {
	case s.queue <- j:
	default:
		s.mu.Unlock()
		return Response{}, errs.New(errs.QueueFull, "The model request queue is full.")
	}
	s.mu.Unlock()

	select {
	case r := <-j.result:
		return r.resp, r.err
	case <-ctx.Done():
		return Response{}, errs.Wrap(errs.Timeout, ctx.Err(), "request cancelled")
	}
}

// QueueLen returns the number of queued (not running) jobs.
func (s *Scheduler) QueueLen() int { return len(s.queue) }

func (s *Scheduler) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stop:
			s.drain()
			return
		case j := <-s.queue:
			if s.stopped() {
				s.reject(j)
				s.drain()
				return
			}
			if j.ctx.Err() != nil {
				// Caller left while queued: skip without inference.
				continue
			}
			queueMS := float64(time.Since(j.enqueued).Microseconds()) / 1000
			// Detach from the caller: in-flight passes always complete.
			resp, err := s.run(context.WithoutCancel(j.ctx), j.req)
			if err == nil {
				resp.Usage.QueueMS = queueMS
			}
			j.result <- jobResult{resp, err}
			if errs.Is(err, errs.RuntimeCrashed) {
				s.Fail(err)
			}
		}
	}
}

func (s *Scheduler) stopped() bool {
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}

func (s *Scheduler) reject(j *job) {
	s.mu.Lock()
	err := s.failErr
	s.mu.Unlock()
	if err == nil {
		err = errs.New(errs.ShuttingDown, "The server is shutting down.")
	}
	j.result <- jobResult{err: err}
}

func (s *Scheduler) drain() {
	for {
		select {
		case j := <-s.queue:
			s.reject(j)
		default:
			return
		}
	}
}

// Fail marks the runner as unusable (for example after an engine crash) and
// rejects all queued and future jobs with err.
func (s *Scheduler) Fail(err error) {
	s.mu.Lock()
	if s.failErr == nil {
		s.failErr = err
	}
	s.mu.Unlock()
	s.stopOnce.Do(func() { close(s.stop) })
}

// Close stops accepting jobs, rejects queued jobs and waits for the running
// job (if any) to finish.
func (s *Scheduler) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.stopOnce.Do(func() { close(s.stop) })
	s.wg.Wait()
}
