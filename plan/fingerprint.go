package plan

import (
	"crypto/sha256"
	"encoding/hex"
)

type Fingerprint [32]byte

func (f Fingerprint) IsZero() bool { return f == Fingerprint{} }
func (f Fingerprint) String() string {
	return hex.EncodeToString(f[:])
}
func (f Fingerprint) Bytes() []byte { return append([]byte(nil), f[:]...) }

func fingerprint(domain string, canonical []byte) Fingerprint {
	hash := sha256.New()
	_, _ = hash.Write([]byte(domain))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(canonical)
	var result Fingerprint
	copy(result[:], hash.Sum(nil))
	return result
}
