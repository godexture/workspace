package scratch

import (
	"math"
	"sync"

	"github.com/godexture/godec/resource"
)

// Budget is the ceiling that growing journals share. A node whose temporary
// size nobody could state in advance reserves nothing, so what bounds it is
// what has actually been written across the whole job -- charged as it is
// written and repaid when the journal closes.
//
// A job may lift the ceiling entirely, which leaves the storage itself as the
// only bound. That is a choice the job makes rather than one a component can
// make for it.
type Budget struct {
	limit     int64
	unlimited bool

	mu   sync.Mutex
	used int64
}

func NewBudget(limit resource.Bytes, unlimited bool) *Budget {
	if uint64(limit) > math.MaxInt64 {
		return &Budget{limit: math.MaxInt64, unlimited: unlimited}
	}
	return &Budget{limit: int64(limit), unlimited: unlimited}
}

func (b *Budget) Used() int64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}

// Enabled reports whether any growing journal may be opened at all. A job that
// left no ceiling and did not lift it has disabled them.
func (b *Budget) Enabled() bool { return b != nil && (b.unlimited || b.limit > 0) }

func (b *Budget) charge(amount int64) error {
	if b == nil {
		return ErrDisabled
	}
	if amount <= 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used > math.MaxInt64-amount {
		return ErrQuota
	}
	if !b.unlimited && b.used+amount > b.limit {
		return ErrQuota
	}
	b.used += amount
	return nil
}

func (b *Budget) repay(amount int64) {
	if b == nil || amount <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.used = max(b.used-amount, 0)
}
