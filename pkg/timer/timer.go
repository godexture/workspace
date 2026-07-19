package timer

import "time"

type Timer struct {
	start time.Time
}

func New() *Timer {
	return &Timer{start: time.Now()}
}

func (t *Timer) Elapsed() time.Duration {
	return time.Since(t.start)
}

func (t *Timer) Reset() {
	t.start = time.Now()
}
