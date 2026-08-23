package stream

import (
	"errors"

	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type durationKeyID struct{}

// duration is how long the stream lasts, counted in its own time base. A
// format states it when it knows: a container that recorded a sample count or
// a track duration knows before it has read a single sample, and a component
// that would otherwise have to hold the whole stream to find the end can then
// let it pass instead.
//
// It is a property rather than a field of Descriptor because plenty of streams
// have no end to state -- anything live, and anything still being written --
// and an absent property says that without a zero every reader has to know to
// distrust.
var duration = property.Define[durationKeyID](func(value timing.Duration) ([]byte, error) {
	if value < 0 {
		return nil, errors.New("stream duration must not be negative")
	}
	return property.Scalar[int64]()(value.Int64())
})

// Duration returns the key a format states a stream's length under.
func Duration() property.Key[timing.Duration] { return duration }

// DurationOf reads the length of a stream, and reports false when the stream
// does not state one.
func DurationOf(properties property.Set) (timing.OptionalDuration, bool) {
	value, ok := duration.Get(properties)
	if !ok {
		return timing.UnknownDuration(), false
	}
	return timing.SomeDuration(value), true
}

// WithDuration records the length of a stream. A component that changes how
// many instants a stream covers restates it; one that cannot say any more
// removes it with WithoutDuration rather than leaving a length that has
// stopped being true.
func WithDuration(properties property.Set, value timing.Duration) (property.Set, error) {
	return duration.Set(properties, value)
}

func WithoutDuration(properties property.Set) property.Set {
	return properties.Delete(duration.ID())
}

// Declarations exposes the stream-level property vocabulary to optional Host
// conflict validation.
func Declarations() []plugin.Declaration {
	return []plugin.Declaration{plugin.DeclareKey(duration)}
}
