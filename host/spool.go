package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/internal/scratch"
)

const spoolCopyBlockSize = 64 * 1024

var errSpoolLimit = errors.New("spool quota exceeded")

type spoolStorage interface {
	WriteAt([]byte, int64) (int, error)
	CopyTo(context.Context, access.Appender, int64) error
	Close() error
}

type spoolState uint8

const (
	spoolWriting spoolState = iota + 1
	spoolCopied
	spoolPrepared
	spoolCommitted
	spoolAborted
	spoolClosed
)

type spoolSession struct {
	mu           sync.Mutex
	spec         access.SpoolSpec
	capabilities access.Capabilities
	storage      spoolStorage
	underlying   access.Session
	appender     access.Appender
	flusher      access.Flusher
	syncer       access.Syncer
	transaction  access.Transaction
	extent       int64
	state        spoolState
	closeOnce    sync.Once
	closeErr     error
}

func newSpoolSession(spec access.SpoolSpec, underlying access.Session, opening access.Opening) (*spoolSession, error) {
	storage, err := newSpoolStorage(spec)
	if err != nil {
		return nil, err
	}
	result, err := newSpoolSessionWithStorage(spec, underlying, opening, storage)
	if err != nil {
		return nil, errors.Join(err, protectedCall("", "spool/storage-close", func() error { return storage.Close() }))
	}
	return result, nil
}

func newSpoolSessionWithStorage(spec access.SpoolSpec, underlying access.Session, opening access.Opening, storage spoolStorage) (*spoolSession, error) {
	if !spec.Valid() || !spec.FinalCopy() || underlying == nil || storage == nil || !opening.Valid() || opening.Direction() != access.SinkDirection {
		return nil, errors.New("spool requires a valid output specification, session, opening, and storage")
	}
	appender, appenderOK := access.AppenderOf(opening)
	flusher, flusherOK := access.FlusherOf(opening)
	syncer, syncerOK := access.SyncerOf(opening)
	transaction, transactionOK := access.TransactionOf(opening)
	if !appenderOK || !flusherOK || !syncerOK || !transactionOK {
		return nil, errors.New("spool underlying opening is missing sequential transaction services")
	}
	values := opening.Selected()
	foundRandom := false
	for _, capability := range values {
		if capability == access.RandomWrite {
			foundRandom = true
			break
		}
	}
	if !foundRandom {
		values = append(values, access.RandomWrite)
	}
	capabilities, err := access.NewCapabilities(values...)
	if err != nil {
		return nil, err
	}
	return &spoolSession{
		spec:         spec,
		capabilities: capabilities,
		storage:      storage,
		underlying:   underlying,
		appender:     appender,
		flusher:      flusher,
		syncer:       syncer,
		transaction:  transaction,
		state:        spoolWriting,
	}, nil
}

func (s *spoolSession) Capabilities() access.Capabilities {
	result, _ := access.NewCapabilities(s.capabilities.Values()...)
	return result
}

func (s *spoolSession) Write(ctx context.Context, source []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeAt(ctx, source, s.extent)
}

func (s *spoolSession) WriteAt(ctx context.Context, source []byte, offset int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeAt(ctx, source, offset)
}

func (s *spoolSession) writeAt(ctx context.Context, source []byte, offset int64) (int, error) {
	if err := spoolContextFailure(ctx); err != nil {
		return 0, err
	}
	if s.state != spoolWriting || s.storage == nil {
		return 0, errors.New("spool is not writable")
	}
	if offset < 0 || int64(len(source)) > math.MaxInt64-offset {
		return 0, errors.New("spool write extent overflows int64")
	}
	end := offset + int64(len(source))
	if end > s.spec.MaximumBytes() {
		return 0, fmt.Errorf("%w: extent %d exceeds %d bytes", errSpoolLimit, end, s.spec.MaximumBytes())
	}
	count, err := s.storage.WriteAt(source, offset)
	if count < 0 || count > len(source) {
		return 0, errors.New("spool storage returned an invalid write count")
	}
	if writtenEnd := offset + int64(count); writtenEnd > s.extent {
		s.extent = writtenEnd
	}
	if cause := spoolContextFailure(ctx); cause != nil {
		return count, cause
	}
	return count, err
}

func (s *spoolSession) Flush(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == spoolCopied || s.state == spoolPrepared || s.state == spoolCommitted {
		return nil
	}
	if s.state != spoolWriting || s.storage == nil {
		return errors.New("spool cannot be flushed in its current state")
	}
	if err := protectedCall("", "spool/copy", func() error {
		return s.storage.CopyTo(ctx, s.appender, s.extent)
	}); err != nil {
		return err
	}
	if err := protectedCall("", "access/flush", func() error { return s.flusher.Flush(ctx) }); err != nil {
		return err
	}
	if err := s.closeStorage(); err != nil {
		return err
	}
	s.state = spoolCopied
	return nil
}

func (s *spoolSession) Sync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != spoolCopied {
		return errors.New("spool must be copied before sync")
	}
	return protectedCall("", "access/sync", func() error { return s.syncer.Sync(ctx) })
}

func (s *spoolSession) PrepareCommit(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != spoolCopied {
		return errors.New("spool must be copied before prepare commit")
	}
	if err := protectedCall("", "access/prepare-commit", func() error { return s.transaction.PrepareCommit(ctx) }); err != nil {
		return err
	}
	s.state = spoolPrepared
	return nil
}

func (s *spoolSession) Commit(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != spoolPrepared {
		return errors.New("spool must be prepared before commit")
	}
	if err := protectedCall("", "access/commit", func() error { return s.transaction.Commit(ctx) }); err != nil {
		return err
	}
	s.state = spoolCommitted
	return nil
}

func (s *spoolSession) Abort(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == spoolCommitted || s.state == spoolAborted || s.state == spoolClosed {
		return nil
	}
	s.state = spoolAborted
	var failures []error
	if err := protectedCall("", "access/abort", func() error { return s.transaction.Abort(ctx) }); err != nil {
		failures = append(failures, err)
	}
	if err := s.closeStorage(); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (s *spoolSession) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		var failures []error
		if err := s.closeStorage(); err != nil {
			failures = append(failures, err)
		}
		if s.underlying != nil {
			underlying := s.underlying
			if err := protectedCall("", "access/session", func() error { return underlying.Close() }); err != nil {
				failures = append(failures, err)
			}
			s.underlying = nil
		}
		s.state = spoolClosed
		s.closeErr = errors.Join(failures...)
	})
	return s.closeErr
}

func (s *spoolSession) closeStorage() error {
	if s.storage == nil {
		return nil
	}
	storage := s.storage
	// Detach before invoking Close. A panic leaves the external outcome
	// unknowable, and retrying could close a non-idempotent resource twice.
	s.storage = nil
	return protectedCall("", "spool/storage-close", storage.Close)
}

func newSpoolStorage(spec access.SpoolSpec) (spoolStorage, error) {
	switch spec.Storage() {
	case access.MemorySpool:
		return &memorySpool{}, nil
	case access.DiskSpool:
		temporary, err := scratch.NewTemporary("godec-spool-*")
		if err != nil {
			return nil, err
		}
		return &diskSpool{temporary: temporary, path: temporary.Path()}, nil
	default:
		return nil, access.ErrInvalidSpoolSpec
	}
}

type memorySpool struct {
	data   []byte
	closed bool
}

func (s *memorySpool) WriteAt(source []byte, offset int64) (int, error) {
	if s.closed {
		return 0, errors.New("memory spool is closed")
	}
	end := offset + int64(len(source))
	maximumInt := int64(^uint(0) >> 1)
	if end > maximumInt {
		return 0, errors.New("memory spool exceeds the platform slice range")
	}
	if int(end) > len(s.data) {
		s.data = append(s.data, make([]byte, int(end)-len(s.data))...)
	}
	return copy(s.data[int(offset):int(end)], source), nil
}

func (s *memorySpool) CopyTo(ctx context.Context, destination access.Appender, extent int64) error {
	if s.closed || extent < 0 || extent > int64(len(s.data)) {
		return errors.New("memory spool extent is invalid")
	}
	return appendAll(ctx, destination, s.data[:int(extent)])
}

func (s *memorySpool) Close() error {
	s.data = nil
	s.closed = true
	return nil
}

type diskSpool struct {
	temporary *scratch.Temporary
	path      string
}

func (s *diskSpool) WriteAt(source []byte, offset int64) (int, error) {
	if s.temporary == nil {
		return 0, errors.New("disk spool is closed")
	}
	return s.temporary.WriteAt(source, offset)
}

func (s *diskSpool) CopyTo(ctx context.Context, destination access.Appender, extent int64) error {
	if s.temporary == nil || extent < 0 {
		return errors.New("disk spool extent is invalid")
	}
	buffer := make([]byte, spoolCopyBlockSize)
	for offset := int64(0); offset < extent; {
		if err := spoolContextFailure(ctx); err != nil {
			return err
		}
		size := int64(len(buffer))
		if remaining := extent - offset; remaining < size {
			size = remaining
		}
		count, err := s.temporary.ReadAt(buffer[:int(size)], offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if count != int(size) {
			return io.ErrUnexpectedEOF
		}
		if err := appendAll(ctx, destination, buffer[:count]); err != nil {
			return err
		}
		offset += int64(count)
	}
	return nil
}

func (s *diskSpool) Close() error {
	if s.temporary == nil {
		return nil
	}
	temporary := s.temporary
	s.temporary = nil
	return temporary.Close()
}

func appendAll(ctx context.Context, destination access.Appender, source []byte) error {
	remaining := source
	for len(remaining) != 0 {
		if err := spoolContextFailure(ctx); err != nil {
			return err
		}
		count, err := destination.Write(ctx, remaining)
		if count < 0 || count > len(remaining) {
			return errors.New("spool destination returned an invalid write count")
		}
		remaining = remaining[count:]
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func spoolContextFailure(ctx context.Context) error {
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
