package fake

import (
	"context"
	"sync"
	"time"

	"github.com/dgarwin/alertme/backend/internal/queue"
)

// EnqueuedTask records one Enqueue call for assertions in tests.
type EnqueuedTask struct {
	Task  queue.PageTask
	Delay time.Duration
}

// Enqueuer is an in-memory queue.Enqueuer: it records tasks instead of
// sending them anywhere, and can be made to fail on demand.
type Enqueuer struct {
	mu    sync.Mutex
	Tasks []EnqueuedTask
	Err   error // if set, Enqueue returns this error instead of recording
}

var _ queue.Enqueuer = (*Enqueuer)(nil)

func (e *Enqueuer) Enqueue(ctx context.Context, task queue.PageTask, delay time.Duration) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.Err != nil {
		return e.Err
	}
	e.Tasks = append(e.Tasks, EnqueuedTask{Task: task, Delay: delay})
	return nil
}

// Last returns the most recently enqueued task, and false if none was.
func (e *Enqueuer) Last() (EnqueuedTask, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.Tasks) == 0 {
		return EnqueuedTask{}, false
	}
	return e.Tasks[len(e.Tasks)-1], true
}
