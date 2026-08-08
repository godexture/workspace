package access

import (
	"errors"
	"time"
)

// SpoolStorage declares where a host-managed spool is expected to reside.
type SpoolStorage uint8

const (
	MemorySpool SpoolStorage = iota + 1
	DiskSpool
)

func (s SpoolStorage) Valid() bool { return s >= MemorySpool && s <= DiskSpool }

var ErrInvalidSpoolSpec = errors.New("access spool specification is invalid")

// SpoolSpec makes a capability adaptation visible to planning. It does not
// allocate storage or start I/O; the first insertion and cleanup consumer is M6.
type SpoolSpec struct {
	predictedBytes int64
	storage        SpoolStorage
	startupLatency time.Duration
	rollback       TransactionClass
}

func NewSpoolSpec(predictedBytes int64, storage SpoolStorage, startupLatency time.Duration, rollback TransactionClass) (SpoolSpec, error) {
	if predictedBytes < 0 || !storage.Valid() || startupLatency < 0 || !rollback.Valid() {
		return SpoolSpec{}, ErrInvalidSpoolSpec
	}
	return SpoolSpec{
		predictedBytes: predictedBytes,
		storage:        storage,
		startupLatency: startupLatency,
		rollback:       rollback,
	}, nil
}

func (s SpoolSpec) Valid() bool {
	return s.storage.Valid() && s.predictedBytes >= 0 && s.startupLatency >= 0 && s.rollback.Valid()
}
func (s SpoolSpec) PredictedBytes() int64           { return s.predictedBytes }
func (s SpoolSpec) Storage() SpoolStorage           { return s.storage }
func (s SpoolSpec) StartupLatency() time.Duration   { return s.startupLatency }
func (s SpoolSpec) RollbackClass() TransactionClass { return s.rollback }
