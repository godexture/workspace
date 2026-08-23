//go:generate go run generate.go

// Package g711 holds the G.711 companding tables. Expanding a byte and
// companding a sample are both table lookups, so the tables are what this
// package exposes: the caller keeps its own loop and never pays a call or a
// branch per sample.
package g711

// Expansion tables turn one companded byte into a linear sample at the full
// scale of its 16-bit container.
func ALawExpansion() *[256]uint16 { return &aLawToLinearTable }
func ULawExpansion() *[256]uint16 { return &uLawToLinearTable }

// Companding tables turn one linear sample into a companded byte.
func ALawCompanding() *[65536]byte { return &linearToALawTable }
func ULawCompanding() *[65536]byte { return &linearToULawTable }
