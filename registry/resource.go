package registry

// ResourceRequest declares execution resources a transform can use. It is
// deliberately independent from Configuration: resources affect scheduling,
// not the semantic output of a plugin.
type ResourceRequest struct {
	Parallelism bool
}

// ResourceBudget is the share assigned to one transform instance.
type ResourceBudget struct {
	Parallelism int
}
