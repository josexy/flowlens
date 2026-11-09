package runtime

import (
	"context"
	"fmt"
	stdruntime "runtime"
	"strings"
	"sync"

	"github.com/josexy/flowlens/backend/pkg/logger"
)

// Jobs runs service calls off the main thread and delivers each result only
// if it is still wanted when it arrives on the main thread.
type Jobs struct {
	dispatcher *Dispatcher

	mu      sync.Mutex
	next    uint64
	running map[string]*runningJob
	closed  bool
	wg      sync.WaitGroup
}

type runningJob struct {
	id     uint64
	cancel context.CancelFunc
}

// NewJobs returns Jobs that deliver results through dispatcher.
func NewJobs(dispatcher *Dispatcher) *Jobs {
	return &Jobs{dispatcher: dispatcher, running: make(map[string]*runningJob)}
}

// Run calls work on its own goroutine and then done on the main thread with
// its result. A later Run with the same key cancels this one, and done is not
// called for a job that was superseded, canceled, or finished after Close.
func Run[T any](jobs *Jobs, key string, work func(context.Context) (T, error), done func(T, error)) {
	ctx, cancel := context.WithCancel(context.Background())

	jobs.mu.Lock()
	if jobs.closed {
		jobs.mu.Unlock()
		cancel()
		return
	}
	if previous := jobs.running[key]; previous != nil {
		previous.cancel()
	}
	jobs.next++
	id := jobs.next
	jobs.running[key] = &runningJob{id: id, cancel: cancel}
	jobs.wg.Add(1)
	jobs.mu.Unlock()

	go func() {
		defer jobs.wg.Done()
		value, err := runWork(ctx, work)
		jobs.dispatcher.Post(func() {
			if jobs.finish(key, id) && done != nil {
				done(value, err)
			}
		})
	}()
}

// runWork turns a panic in a service call into an error, so that one failed
// operation reports a failure instead of ending the application.
func runWork[T any](ctx context.Context, work func(context.Context) (T, error)) (value T, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			stack := make([]byte, 16<<10)
			stack = stack[:stdruntime.Stack(stack, false)]
			logger.G().Errorf("Background job panicked: %v\n%s", recovered, stack)
			err = fmt.Errorf("internal error: %v", recovered)
		}
	}()
	return work(ctx)
}

func (j *Jobs) finish(key string, id uint64) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	current := j.running[key]
	if current == nil || current.id != id {
		return false
	}
	current.cancel()
	delete(j.running, key)
	return true
}

// Active reports whether the job for key has not finished.
func (j *Jobs) Active(key string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.running[key] != nil
}

// Cancel abandons the job for key: its context is canceled and its result is
// discarded.
func (j *Jobs) Cancel(key string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if current := j.running[key]; current != nil {
		current.cancel()
		delete(j.running, key)
	}
}

// CancelPrefix abandons every job whose key starts with prefix, such as the
// jobs that belong to a window being closed.
func (j *Jobs) CancelPrefix(prefix string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for key, current := range j.running {
		if strings.HasPrefix(key, prefix) {
			current.cancel()
			delete(j.running, key)
		}
	}
}

// Wait blocks until every job's work has returned.
func (j *Jobs) Wait() { j.wg.Wait() }

// Close abandons all jobs, refuses new ones, and waits for running work to
// return.
func (j *Jobs) Close() {
	j.mu.Lock()
	j.closed = true
	for key, current := range j.running {
		current.cancel()
		delete(j.running, key)
	}
	j.mu.Unlock()
	j.wg.Wait()
}
