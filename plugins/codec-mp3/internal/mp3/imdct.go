package mp3

/*
#cgo CFLAGS: -O3 -DMINIMP3_FLOAT_OUTPUT
#include "minimp3.h"

void mp3dec_imdct_gr_c(float *grbuf, float *overlap, int block_type, int n_long_bands);
*/
import "C"
import "unsafe"

// L3Imdct performs IMDCT on a granule block by calling the C minimp3 implementation.
func L3Imdct(grbuf []float32, overlap []float32, blockType int, nLongBands int) {
	if len(grbuf) == 0 || len(overlap) == 0 {
		return
	}
	C.mp3dec_imdct_gr_c(
		(*C.float)(unsafe.Pointer(&grbuf[0])),
		(*C.float)(unsafe.Pointer(&overlap[0])),
		C.int(blockType),
		C.int(nLongBands),
	)
}

