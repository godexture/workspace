package registry

// ResourceRequest declares execution resources a transform can use. It is
// deliberately independent from Configuration: resources affect scheduling,
// not the semantic output of a plugin.
type ResourceRequest struct {
	Parallelism bool
}

// ResourceBudget is the total execution budget requested for a conversion.
type ResourceBudget struct {
	Parallelism int
}

// ResourceGrant is what one transform instance actually receives after
// negotiation. Pool is nil when the transform did not request Parallelism;
// otherwise it is a shared handle also held by every other parallel-eligible
// stage in the same conversion, so capacity moves to whichever stage has
// runnable work instead of being split evenly up front.
type ResourceGrant struct {
	Pool *WorkerPool
}

// Parallelism reports the shared pool's total worker count, or 0 if Pool is
// nil. This is informational: unlike a per-stage exclusive share, it is the
// same value for every stage granted the same pool.
func (g ResourceGrant) Parallelism() int {
	if g.Pool == nil {
		return 0
	}
	return g.Pool.Size()
}
