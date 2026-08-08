//go:build !goexperiment.simd || !amd64

package dsp

func HasAVX2() bool    { return false }
func HasAVX2FMA() bool { return false }
