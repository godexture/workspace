//go:build !goexperiment.simd || !amd64

package dsp

const (
	HasAVX2    = false
	HasAVX2FMA = false
)
