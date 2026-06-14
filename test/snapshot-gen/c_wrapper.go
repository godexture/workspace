//go:build cgo_test

package main

/*
#cgo CFLAGS: -O0
#include "minimp3.h"

void mp3dec_imdct_gr_c(float *grbuf, float *overlap, int block_type, int n_long_bands);
void mp3dec_synth_granule_c(float *qmf_state, float *grbuf, int nbands, int nch, float *pcm, float *lins);
*/
import "C"
import "unsafe"

func C_imdct(grbuf []float32, overlap []float32, blockType int, nLongBands int) {
	C.mp3dec_imdct_gr_c(
		(*C.float)(unsafe.Pointer(&grbuf[0])),
		(*C.float)(unsafe.Pointer(&overlap[0])),
		C.int(blockType),
		C.int(nLongBands),
	)
}

func C_synth_granule(qmfState []float32, grbuf []float32, nbands int, nch int, pcm []float32, lins []float32) {
	C.mp3dec_synth_granule_c(
		(*C.float)(unsafe.Pointer(&qmfState[0])),
		(*C.float)(unsafe.Pointer(&grbuf[0])),
		C.int(nbands),
		C.int(nch),
		(*C.float)(unsafe.Pointer(&pcm[0])),
		(*C.float)(unsafe.Pointer(&lins[0])),
	)
}
