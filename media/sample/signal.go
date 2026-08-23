package sample

import (
	"errors"
	"fmt"

	"github.com/godexture/godec/media/property"
)

// Signal is what an audio stream is, independent of how its samples are
// stored: how many of them per second, across which channels, and how many
// bits of each carry information. Decoding never changes any of the three, so
// every audio stream states a Signal whether its samples are stored one scalar
// each, companded, or compressed.
type Signal struct {
	Rate   int
	Layout Layout
	// ValidBits is the depth of the signal itself, not of the container that
	// holds it. It is zero when the stream does not state a depth: a companded
	// or compressed stream usually cannot, because its container width and its
	// signal depth are different numbers and only its codec knows the second.
	ValidBits int
}

var ErrInvalidSignal = errors.New("audio signal is invalid")

func (s Signal) Valid() bool {
	return s.Rate > 0 && s.Layout.Valid() && s.ValidBits >= 0 && s.ValidBits <= maxValidBits
}

// maxValidBits is the widest signal any canonical coding can carry.
const maxValidBits = 64

// Properties encodes the signal into an immutable descriptor property set.
func (s Signal) Properties() (property.Set, error) { return s.Apply(property.New()) }

// Apply replaces the signal properties while preserving every other property
// already present. An unstated depth leaves the valid-bits key absent rather
// than writing a zero every reader would have to special-case, and removes a
// depth the set carried before, so the result never states one this signal
// does not.
func (s Signal) Apply(result property.Set) (property.Set, error) {
	if !s.Valid() {
		return property.Set{}, ErrInvalidSignal
	}
	accumulator := putter{set: result}
	put(&accumulator, sampleRate, s.Rate)
	put(&accumulator, channelLayout, s.Layout)
	if s.ValidBits != 0 {
		put(&accumulator, validBits, s.ValidBits)
	} else {
		accumulator.set = accumulator.set.Delete(validBits.ID())
	}
	if accumulator.err != nil {
		return property.Set{}, accumulator.err
	}
	return accumulator.set, nil
}

// SignalOf decodes the signal every audio stream states. It succeeds for a
// compressed stream that names no storage representation.
func SignalOf(properties property.Set) (Signal, error) {
	rate, rateOK := sampleRate.Get(properties)
	layout, layoutOK := channelLayout.Get(properties)
	bits, _ := validBits.Get(properties)
	result := Signal{Rate: rate, Layout: layout, ValidBits: bits}
	if !rateOK || !layoutOK || !result.Valid() {
		return Signal{}, fmt.Errorf("%w: audio signal properties are incomplete or inconsistent", ErrInvalidSignal)
	}
	return result, nil
}
