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
	maximumBytes   int64
	predictedBytes int64
	storage        SpoolStorage
	startupLatency time.Duration
	finalCopy      bool
	rollback       TransactionClass
}

func NewSpoolSpec(maximumBytes, predictedBytes int64, storage SpoolStorage, startupLatency time.Duration, finalCopy bool, rollback TransactionClass) (SpoolSpec, error) {
	if maximumBytes <= 0 || predictedBytes < 0 || predictedBytes > maximumBytes || !storage.Valid() || startupLatency < 0 || !rollback.Valid() {
		return SpoolSpec{}, ErrInvalidSpoolSpec
	}
	return SpoolSpec{
		maximumBytes:   maximumBytes,
		predictedBytes: predictedBytes,
		storage:        storage,
		startupLatency: startupLatency,
		finalCopy:      finalCopy,
		rollback:       rollback,
	}, nil
}

func (s SpoolSpec) Valid() bool {
	return s.maximumBytes > 0 && s.predictedBytes >= 0 && s.predictedBytes <= s.maximumBytes && s.storage.Valid() && s.startupLatency >= 0 && s.rollback.Valid()
}
func (s SpoolSpec) IsZero() bool                    { return s == (SpoolSpec{}) }
func (s SpoolSpec) MaximumBytes() int64             { return s.maximumBytes }
func (s SpoolSpec) PredictedBytes() int64           { return s.predictedBytes }
func (s SpoolSpec) Storage() SpoolStorage           { return s.storage }
func (s SpoolSpec) StartupLatency() time.Duration   { return s.startupLatency }
func (s SpoolSpec) FinalCopy() bool                 { return s.finalCopy }
func (s SpoolSpec) RollbackClass() TransactionClass { return s.rollback }
