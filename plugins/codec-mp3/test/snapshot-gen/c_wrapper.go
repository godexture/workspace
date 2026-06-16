//go:build cgo_test

package main

/*
#cgo CFLAGS: -O0
#include "minimp3.h"

void mp3dec_imdct_gr_c(float *grbuf, float *overlap, int block_type, int n_long_bands);
void mp3dec_synth_granule_c(float *qmf_state, float *grbuf, int nbands, int nch, int16_t *pcm, float *lins);
*/
import "C"
import "unsafe"

func C_imdct(granule []float32, overlap []float32, blockType int, longBandCount int) {
	C.mp3dec_imdct_gr_c(
		(*C.float)(unsafe.Pointer(&granule[0])),
		(*C.float)(unsafe.Pointer(&overlap[0])),
		C.int(blockType),
		C.int(longBandCount),
	)
}

func C_synth_granule(quadratureMirrorFilterState []float32, granule []float32, bandCount int, channelCount int, pcmSamples []int16, workspace []float32) {
	C.mp3dec_synth_granule_c(
		(*C.float)(unsafe.Pointer(&quadratureMirrorFilterState[0])),
		(*C.float)(unsafe.Pointer(&granule[0])),
		C.int(bandCount),
		C.int(channelCount),
		(*C.int16_t)(unsafe.Pointer(&pcmSamples[0])),
		(*C.float)(unsafe.Pointer(&workspace[0])),
	)
}
