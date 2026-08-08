// Package resource defines planning-time resource requests, estimates, and
// grants. Runtime allocation and accounting remain private to Host.
package resource

import "time"

// Bytes and Work are non-negative planning units. Work is a relative amount
// that may compare implementations of the same semantic operation; it is not
// a correctness score.
type Bytes uint64
type Work uint64

// Confidence states how precisely an estimate is known.
type Confidence uint8

const (
	UnknownConfidence Confidence = iota
	EstimatedConfidence
	BoundedConfidence
	ExactConfidence
)

func (c Confidence) Valid() bool { return c <= ExactConfidence }

// Request is the minimum grant needed to open one compiled component.
type Request struct {
	Memory    Bytes
	Temporary Bytes
	Workers   uint32
	Queue     uint32
}

// Estimate describes expected physical cost. It never authorizes allocation.
type Estimate struct {
	CPU        Work
	Memory     Bytes
	Latency    time.Duration
	Buffering  time.Duration
	Confidence Confidence
}

func (e Estimate) Valid() bool {
	return e.Latency >= 0 && e.Buffering >= 0 && e.Confidence.Valid()
}

// Grant is the bounded capacity assigned by a Host or Job owner.
type Grant struct {
	Memory    Bytes
	Temporary Bytes
	Workers   uint32
	Queue     uint32
}

// Satisfies reports whether every requested dimension fits in the grant.
func (g Grant) Satisfies(request Request) bool {
	return g.Memory >= request.Memory &&
		g.Temporary >= request.Temporary &&
		g.Workers >= request.Workers &&
		g.Queue >= request.Queue
}
