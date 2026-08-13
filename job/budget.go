package job

import (
	"math"
	"time"

	"github.com/godexture/godec/resource"
)

// Budget bounds planner work independently of context cancellation. Duration
// is a defensive wall-clock limit; deterministic state limits remain the
// reproducible termination contract.
type Budget struct {
	States             int
	Compiles           int
	SuggestionsPerNeed int
	FixpointIterations int
	ProbeBytes         resource.Bytes
	ProbeRounds        int
	// InspectBytes bounds what the selected Format may read from one source
	// while establishing its stream descriptor. Container headers and their
	// preserved metadata are read here, so the limit is what keeps a declared
	// chunk size from sizing an allocation.
	InspectBytes resource.Bytes
	Duration     time.Duration
}

func (b Budget) Valid() bool {
	return b.States > 0 && b.Compiles > 0 && b.SuggestionsPerNeed > 0 && b.FixpointIterations > 0 &&
		b.ProbeBytes > 0 && uint64(b.ProbeBytes) <= math.MaxInt64 && b.ProbeRounds > 0 &&
		b.InspectBytes > 0 && uint64(b.InspectBytes) <= math.MaxInt64 && b.Duration >= 0
}

func DefaultBudget() Budget {
	return Budget{
		States:             4096,
		Compiles:           16384,
		SuggestionsPerNeed: 64,
		FixpointIterations: 64,
		ProbeBytes:         64 << 10,
		ProbeRounds:        16,
		InspectBytes:       16 << 20,
	}
}
