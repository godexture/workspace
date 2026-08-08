//go:build goexperiment.simd && amd64

package dsp

import (
	"os"
	"simd/archsimd"
)

// forceScalarEnv, when set to any non-empty value, makes HasAVX2/HasAVX2FMA
// report false even on hardware that actually has the feature. This lets a
// SIMD-capable build exercise every scalar dispatch path it contains without
// a separate scalar build, so a cross-build differential can compare
// scalar-build, natural SIMD-build, and forced-scalar-within-SIMD-build
// results instead of only the first two. See tools/cmd/differential.
const forceScalarEnv = "GODEC_FORCE_SCALAR"

var (
	hasAVX2    = archsimd.X86.AVX2() && !forcedScalar()
	hasAVX2FMA = archsimd.X86.AVX2() && archsimd.X86.FMA() && !forcedScalar()
)

func HasAVX2() bool    { return hasAVX2 }
func HasAVX2FMA() bool { return hasAVX2FMA }

func forcedScalar() bool {
	return os.Getenv(forceScalarEnv) != ""
}
