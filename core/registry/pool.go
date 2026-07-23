package registry

import "sync"

// WorkerPool is a bounded set of goroutines shared by every pipeline stage in
// a single conversion run that requested Parallelism. Capacity flows to
// whichever stage currently has runnable work: Submit blocks once the queue
// and all workers are busy, so a stage sitting idle never holds capacity that
// a busy stage could use instead.
type WorkerPool struct {
	tasks     chan func()
	wg        sync.WaitGroup
	size      int
	closeOnce sync.Once
}

// NewWorkerPool starts size goroutines draining a shared task queue. size is
// clamped to at least 1.
func NewWorkerPool(size int) *WorkerPool {
	if size < 1 {
		size = 1
	}
	pool := &WorkerPool{tasks: make(chan func(), 2*size), size: size}
	pool.wg.Add(size)
	for range size {
		go func() {
			defer pool.wg.Done()
			for task := range pool.tasks {
				task()
			}
		}()
	}
	return pool
}

// Submit runs task on a pool worker, blocking until a worker or queue slot is
// free.
func (p *WorkerPool) Submit(task func()) {
	p.tasks <- task
}

// Size reports the number of worker goroutines backing the pool.
func (p *WorkerPool) Size() int {
	return p.size
}

// Close stops accepting new work and waits for in-flight tasks to finish.
// Callers must ensure every stage sharing this pool has stopped submitting
// before calling Close. Safe to call more than once, and from more than one
// of a stage's teardown hooks (e.g. both a normal end-of-stream flush and a
// later unconditional close).
func (p *WorkerPool) Close() error {
	p.closeOnce.Do(func() {
		close(p.tasks)
		p.wg.Wait()
	})
	return nil
}
