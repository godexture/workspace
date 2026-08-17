package scratch

import (
	"errors"
	"os"
	"sync"
)

// Temporary is one Host-owned disk file. It closes and removes its file at
// most once, including when close and removal both fail.
type Temporary struct {
	handle *os.File
	path   string

	closeOnce sync.Once
	closeErr  error
}

func NewTemporary(pattern string) (*Temporary, error) {
	handle, err := os.CreateTemp("", pattern)
	if err != nil {
		return nil, err
	}
	return &Temporary{handle: handle, path: handle.Name()}, nil
}

// Path is available to the Host's other temporary-file adapter only for its
// own lifecycle assertions; plugin Scratch never exposes it.
func (t *Temporary) Path() string {
	if t == nil {
		return ""
	}
	return t.path
}

func (t *Temporary) ReadAt(target []byte, offset int64) (int, error) {
	if t == nil || t.handle == nil {
		return 0, errors.New("temporary file is closed")
	}
	return t.handle.ReadAt(target, offset)
}

func (t *Temporary) WriteAt(source []byte, offset int64) (int, error) {
	if t == nil || t.handle == nil {
		return 0, errors.New("temporary file is closed")
	}
	return t.handle.WriteAt(source, offset)
}

func (t *Temporary) Close() error {
	if t == nil {
		return nil
	}
	t.closeOnce.Do(func() {
		var failures []error
		if t.handle != nil {
			failures = append(failures, t.handle.Close())
			t.handle = nil
		}
		if t.path != "" {
			if err := os.Remove(t.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				failures = append(failures, err)
			}
			t.path = ""
		}
		t.closeErr = errors.Join(failures...)
	})
	return t.closeErr
}
