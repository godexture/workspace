//go:build goexperiment.simd && amd64

package dsp

import "simd/archsimd"

var (
	HasAVX2    = archsimd.X86.AVX2()
	HasAVX2FMA = archsimd.X86.AVX2() && archsimd.X86.FMA()
)
