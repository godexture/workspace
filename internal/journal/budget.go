package journal

// Budget bounds what one run's evidence may cost.
//
// It bounds detail, never facts. Every failure that happens is counted, and
// every place detail was dropped says so; what a budget decides is how many
// full copies of a repeated failure the run keeps, not whether the run admits
// the failure happened. A hundred thousand payloads that could not be released
// is one class, one count, a couple of samples, and an explicit statement that
// the rest were omitted -- never a hundred thousand stacks, and never silence.
//
// The numbers are logical record and byte counts rather than measured heap, so
// the same input produces the same evidence on every machine.
type Budget struct {
	// Events caps all representative event copies kept by the ledger. It applies
	// equally to work and cleanup occurrences: one joined error can contain
	// arbitrarily many independent work occurrences, so exempting work would not
	// be a bound. The stop cause is held separately as one provenance record.
	Events int
	// GroupSamples caps how many full copies one failure class keeps.
	GroupSamples int
	// Groups caps how many distinct failure classes are tracked separately
	// before the rest fold into a coarser overflow class. Without it, an error
	// whose class varies could grow the class table instead of the event list.
	// At most one further group exists beside them: the overflow class itself.
	// It also caps the set of folded classes the overflow group counts exactly
	// before reporting a lower bound.
	Groups int
	// Stacks and StackBytes cap the interned stack depot.
	Stacks     int
	StackBytes int
}

// DefaultBudget is the single place the run's evidence limits are decided.
//
// The defaults are sized so an ordinary failing run keeps everything in full --
// a handful of failures never reaches any of these -- and only a storm is
// summarised.
func DefaultBudget() Budget {
	return Budget{
		Events:       64,
		GroupSamples: 2,
		Groups:       32,
		Stacks:       64,
		StackBytes:   256 << 10,
	}
}

// normalize keeps a caller-supplied budget usable. Zero means "keep none",
// which is a legitimate setting a test uses to reach the truncation paths;
// negative means the same rather than an accidental unbounded ledger.
func (b Budget) normalize() Budget {
	if b.Events < 0 {
		b.Events = 0
	}
	if b.GroupSamples < 0 {
		b.GroupSamples = 0
	}
	if b.Groups < 0 {
		b.Groups = 0
	}
	if b.Stacks < 0 {
		b.Stacks = 0
	}
	if b.StackBytes < 0 {
		b.StackBytes = 0
	}
	return b
}

// add is saturating. A count that wrapped would understate what happened, and
// understating is the one thing this whole design exists to prevent.
func add(count uint64, delta uint64) uint64 {
	if count > ^uint64(0)-delta {
		return ^uint64(0)
	}
	return count + delta
}
