package stream

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"

	"github.com/godexture/godec/internal/gotype"
)

// Fingerprint identifies the canonical planning state of one stream
// descriptor. Metadata values are deliberately not encoded: arbitrary
// metadata keys do not require canonical encoders. Availability and scope
// participate, while preservation and loss remain explicit Effect and Plan
// facts.
type Fingerprint [32]byte

func (f Fingerprint) IsZero() bool { return f == Fingerprint{} }
func (f Fingerprint) String() string {
	return hex.EncodeToString(f[:])
}
func (f Fingerprint) Bytes() []byte { return append([]byte(nil), f[:]...) }

func (d Descriptor) Fingerprint() (Fingerprint, error) {
	if !d.Valid() {
		return Fingerprint{}, ErrInvalidDescriptor
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("godec/stream/fingerprint/v4\x00"))
	writeStatePart(hash, []byte(d.id.String()))
	writeStatePart(hash, []byte(d.schema.Identity().String()))
	writeStatePart(hash, []byte(gotype.Canonical(d.schema.Payload())))
	if d.schema.HasTime() {
		writeStatePart(hash, []byte{1})
	} else {
		writeStatePart(hash, []byte{0})
	}
	var timeBase [16]byte
	binary.BigEndian.PutUint64(timeBase[:8], uint64(d.timeBase.Numerator))
	binary.BigEndian.PutUint64(timeBase[8:], uint64(d.timeBase.Denominator))
	writeStatePart(hash, timeBase[:])
	properties := d.properties.Fingerprint()
	writeStatePart(hash, properties[:])
	writeStatePart(hash, []byte{byte(d.metadata.State()), byte(d.metadata.Scope())})
	var result Fingerprint
	copy(result[:], hash.Sum(nil))
	return result, nil
}

// SameState confirms equality after a fingerprint match. It compares exactly
// the fields included by Fingerprint so hash collisions never merge states.
func (d Descriptor) SameState(other Descriptor) bool {
	return d.id == other.id &&
		d.schema.Equal(other.schema) &&
		d.timeBase == other.timeBase &&
		d.properties.Equal(other.properties) &&
		d.metadata.SameState(other.metadata)
}

func writeStatePart(hash interface{ Write([]byte) (int, error) }, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = hash.Write(size[:])
	_, _ = hash.Write(value)
}
