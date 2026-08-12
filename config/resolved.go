package config

import (
	"crypto/sha256"
	"fmt"
	"sort"

	"github.com/godexture/godec/diagnostic"
)

// Source records which stage supplied the final field value.
type Source uint8

const (
	SourceDefault Source = iota
	SourcePreset
	SourceExplicit
	SourcePlanner
	SourceNormalized
)

func (s Source) String() string {
	switch s {
	case SourceDefault:
		return "default"
	case SourcePreset:
		return "preset"
	case SourceExplicit:
		return "explicit"
	case SourcePlanner:
		return "planner"
	case SourceNormalized:
		return "normalized"
	default:
		return "unknown"
	}
}

// ProvenanceEntry is one immutable field provenance entry.
type ProvenanceEntry struct {
	Field  string
	Source Source
}

// Provenance identifies the source of every registered field.
type Provenance struct {
	sources map[string]Source
}

// Source returns the source associated with field.
func (p Provenance) Source(field string) (Source, bool) {
	source, ok := p.sources[field]
	return source, ok
}

// Entries returns sorted field provenance without exposing internal state.
func (p Provenance) Entries() []ProvenanceEntry {
	entries := make([]ProvenanceEntry, 0, len(p.sources))
	for field, source := range p.sources {
		entries = append(entries, ProvenanceEntry{Field: field, Source: source})
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Field < entries[right].Field })
	return entries
}

func cloneProvenance(provenance Provenance) Provenance {
	result := Provenance{sources: make(map[string]Source, len(provenance.sources))}
	for field, source := range provenance.sources {
		result.sources[field] = source
	}
	return result
}

// Fingerprint is a domain-separated SHA-256 digest of a canonical resolved
// config. SHA-256 is used for its stable, widely implemented cross-process
// representation; this is an identity hash, not a password hash.
type Fingerprint [32]byte

// IsZero reports whether f has not been computed.
func (f Fingerprint) IsZero() bool { return f == Fingerprint{} }

// String returns the lowercase hexadecimal fingerprint.
func (f Fingerprint) String() string {
	return fmt.Sprintf("%x", f[:])
}

// Bytes returns a copy of the digest bytes.
func (f Fingerprint) Bytes() []byte {
	result := make([]byte, len(f))
	copy(result, f[:])
	return result
}

// Resolved is the immutable control-plane result of schema resolution. The
// Value is a defensive snapshot; callers must treat it as read-only after
// resolution.
type Resolved[C any] struct {
	Value       C
	Provenance  Provenance
	Diagnostics []diagnostic.Item
	Fingerprint Fingerprint
}

// String intentionally reports only identity metadata and never renders the
// value, which keeps SecretValue fields out of logs by default.
func (r Resolved[C]) String() string {
	return "resolved config " + r.Fingerprint.String()
}

func hashCanonical(canonical []byte) Fingerprint {
	hash := sha256.New()
	hash.Write([]byte("godec/config/fingerprint/v1\x00"))
	hash.Write(canonical)
	var result Fingerprint
	copy(result[:], hash.Sum(nil))
	return result
}
