package job

import "time"

// Budget bounds planner work independently of context cancellation. Duration
// is a defensive wall-clock limit; deterministic state limits remain the
// reproducible termination contract.
type Budget struct {
	States             int
	Compiles           int
	SuggestionsPerNeed int
	FixpointIterations int
	Duration           time.Duration
}

func (b Budget) Valid() bool {
	return b.States > 0 && b.Compiles > 0 && b.SuggestionsPerNeed > 0 && b.FixpointIterations > 0 && b.Duration >= 0
}

func DefaultBudget() Budget {
	return Budget{
		States:             4096,
		Compiles:           16384,
		SuggestionsPerNeed: 64,
		FixpointIterations: 64,
	}
}
