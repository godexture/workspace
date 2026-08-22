// Package scratch owns Host-managed temporary byte reservations and journals.
package scratch

import (
	"errors"
	"math"

	"github.com/godexture/godec/resource"
)

var (
	ErrDisabled = errors.New("scratch is disabled")
	ErrOverflow = errors.New("scratch reservation overflows")
	ErrLimit    = errors.New("scratch reservation exceeds its limit")
)

// Reservation is the fixed aggregate temporary-byte budget of one prepared
// job. It is intentionally separate from payload resource grants.
type Reservation struct {
	limit    resource.Bytes
	reserved resource.Bytes
}

func (r Reservation) Limit() resource.Bytes    { return r.limit }
func (r Reservation) Reserved() resource.Bytes { return r.reserved }

// Reserve validates every fixed temporary claim before any temporary file or
// output session is acquired.
func Reserve(limit resource.Bytes, claims ...resource.Bytes) (Reservation, error) {
	if uint64(limit) > math.MaxInt64 {
		return Reservation{}, ErrOverflow
	}
	var total resource.Bytes
	for _, claim := range claims {
		if uint64(claim) > math.MaxInt64 || uint64(total) > math.MaxInt64-uint64(claim) {
			return Reservation{}, ErrOverflow
		}
		total += claim
	}
	if total == 0 {
		return Reservation{limit: limit}, nil
	}
	if limit == 0 {
		return Reservation{}, ErrDisabled
	}
	if total > limit {
		return Reservation{}, ErrLimit
	}
	return Reservation{limit: limit, reserved: total}, nil
}
