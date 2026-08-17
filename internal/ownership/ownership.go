// Package ownership carries the private opt-in face used by flow.Item's
// runtime ownership audit. It deliberately does not expose the tracking
// method: a Reporter from outside this package cannot opt in by accidentally
// declaring a method with the same name.
package ownership

// Reporter is the narrow failure-reporting face an ownership slot retains.
// It is repeated here rather than importing flow or journal so this package
// stays a dependency-free bridge between those two layers.
type Reporter interface {
	Cleanup(error)
}

// tracked is intentionally unimplementable outside this package. An
// unexported method's package identity is part of its method identity, so a
// third-party Reporter with a similarly named exported method cannot opt into
// audit callbacks by accident.
type tracked interface {
	track(int64)
}

type face struct {
	Reporter
	trackFn func(int64)
}

// Wrap returns a Reporter that forwards failures to reporter and accepts
// ownership transitions through the private audit face. A nil callback keeps
// the original Reporter: there is no audit state to maintain in that case.
func Wrap(reporter Reporter, track func(int64)) Reporter {
	if reporter == nil || track == nil {
		return reporter
	}
	return &face{Reporter: reporter, trackFn: track}
}

// Enabled reports whether reporter was created by Wrap.
func Enabled(reporter Reporter) bool {
	if reporter == nil {
		return false
	}
	_, ok := reporter.(tracked)
	return ok
}

// Track forwards one transition to a wrapped Reporter. A Reporter is outside
// the ownership package's trust boundary, so an audit callback panic is
// ignored and the Item operation that caused it remains unchanged.
func Track(reporter Reporter, delta int64) {
	if reporter == nil || delta == 0 {
		return
	}
	trackedReporter, ok := reporter.(tracked)
	if !ok {
		return
	}
	defer func() { _ = recover() }()
	trackedReporter.track(delta)
}

func (f *face) track(delta int64) {
	if f == nil || f.trackFn == nil {
		return
	}
	f.trackFn(delta)
}
