package scratch

import (
	"context"
	"errors"
	"io"
	"math"
	"sync"

	"github.com/godexture/godec/resource"
)

var (
	ErrClosed = errors.New("scratch journal is closed")
	ErrExtent = errors.New("scratch operation is outside the written extent")
	ErrQuota  = errors.New("scratch quota exceeded")
)

// Journal is the narrow node-local scratch service. It serializes full-record
// appends and only permits in-place patches of bytes already appended.
type Journal struct {
	temporary *Temporary
	file      journalFile
	maximum   int64

	mu       sync.Mutex
	extent   int64
	failure  error
	closed   bool
	closeErr error
}

type journalFile interface {
	ReadAt([]byte, int64) (int, error)
	WriteAt([]byte, int64) (int, error)
	Close() error
}

func Open(maximum resource.Bytes) (*Journal, error) {
	if maximum == 0 || uint64(maximum) > math.MaxInt64 {
		return nil, ErrQuota
	}
	temporary, err := NewTemporary("godec-scratch-*")
	if err != nil {
		return nil, err
	}
	return &Journal{temporary: temporary, file: temporary, maximum: int64(maximum)}, nil
}

func (j *Journal) Append(ctx context.Context, source []byte) (int64, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.file == nil {
		return 0, ErrClosed
	}
	if j.failure != nil {
		return 0, j.failure
	}
	if err := contextFailure(ctx); err != nil {
		return 0, err
	}
	if int64(len(source)) > math.MaxInt64-j.extent {
		return 0, ErrQuota
	}
	offset := j.extent
	end := offset + int64(len(source))
	if end > j.maximum {
		return 0, ErrQuota
	}
	count, err := j.file.WriteAt(source, offset)
	if count < 0 || count > len(source) {
		return 0, j.poison(io.ErrShortWrite)
	}
	if err == nil && count != len(source) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return 0, j.poison(err)
	}
	if cause := contextFailure(ctx); cause != nil {
		return 0, j.poison(cause)
	}
	j.extent = end
	return offset, nil
}

func (j *Journal) ReadAt(ctx context.Context, target []byte, offset int64) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.file == nil {
		return ErrClosed
	}
	if j.failure != nil {
		return j.failure
	}
	if err := contextFailure(ctx); err != nil {
		return err
	}
	if !withinExtent(offset, len(target), j.extent) {
		return ErrExtent
	}
	count, err := j.file.ReadAt(target, offset)
	if count < 0 || count > len(target) {
		return j.poison(io.ErrUnexpectedEOF)
	}
	if count == len(target) {
		err = nil
	} else if err == nil {
		err = io.ErrUnexpectedEOF
	}
	if err != nil {
		return j.poison(err)
	}
	if cause := contextFailure(ctx); cause != nil {
		return j.poison(cause)
	}
	return nil
}

func (j *Journal) WriteAt(ctx context.Context, source []byte, offset int64) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.file == nil {
		return ErrClosed
	}
	if j.failure != nil {
		return j.failure
	}
	if err := contextFailure(ctx); err != nil {
		return err
	}
	if !withinExtent(offset, len(source), j.extent) {
		return ErrExtent
	}
	count, err := j.file.WriteAt(source, offset)
	if count < 0 || count > len(source) {
		return j.poison(io.ErrShortWrite)
	}
	if count != len(source) && err == nil {
		err = io.ErrShortWrite
	}
	if err != nil {
		return j.poison(err)
	}
	if cause := contextFailure(ctx); cause != nil {
		return j.poison(cause)
	}
	return nil
}

func (j *Journal) Close() error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return j.closeErr
	}
	j.closed = true
	if j.file != nil {
		j.closeErr = j.file.Close()
		j.file = nil
		j.temporary = nil
	}
	return j.closeErr
}

func withinExtent(offset int64, length int, extent int64) bool {
	return offset >= 0 && int64(length) <= math.MaxInt64-offset && offset+int64(length) <= extent
}

func (j *Journal) poison(err error) error {
	if err != nil && j.failure == nil {
		j.failure = err
	}
	return j.failure
}

func contextFailure(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	default:
		return nil
	}
}
