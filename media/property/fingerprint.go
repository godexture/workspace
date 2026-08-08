package property

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// Fingerprint is the canonical identity of an immutable property set.
type Fingerprint [32]byte

func (f Fingerprint) IsZero() bool { return f == Fingerprint{} }
func (f Fingerprint) String() string {
	return hex.EncodeToString(f[:])
}
func (f Fingerprint) Bytes() []byte { return append([]byte(nil), f[:]...) }

// Fingerprint returns a deterministic digest of sorted key identities and the
// canonical bytes validated when each value entered the set.
func (s Set) Fingerprint() Fingerprint {
	hash := sha256.New()
	_, _ = hash.Write([]byte("godec/property/fingerprint/v1\x00"))
	for _, id := range s.Keys() {
		value := s.values[id]
		writeFingerprintPart(hash, []byte(id.String()))
		writeFingerprintPart(hash, value.canonical)
	}
	var result Fingerprint
	copy(result[:], hash.Sum(nil))
	return result
}

func writeFingerprintPart(hash interface{ Write([]byte) (int, error) }, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = hash.Write(size[:])
	_, _ = hash.Write(value)
}
