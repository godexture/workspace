package mp3

/*
#cgo CFLAGS: -O3 -DMINIMP3_FLOAT_OUTPUT
#include "minimp3.h"

void mp3dec_huffman_c(float *dst, void *bs, const void *gr_info, const float *scf, int layer3gr_limit);
*/
import "C"
import "unsafe"

// L3HuffmanDecode performs Huffman decoding for a Layer 3 granule by calling the C minimp3 implementation.
func L3HuffmanDecode(dst []float32, bs unsafe.Pointer, grInfo unsafe.Pointer, scf []float32, regionLimit int) {
	if len(dst) == 0 || bs == nil || grInfo == nil {
		return
	}
	var scfPtr *C.float
	if len(scf) > 0 {
		scfPtr = (*C.float)(unsafe.Pointer(&scf[0]))
	}
	C.mp3dec_huffman_c(
		(*C.float)(unsafe.Pointer(&dst[0])),
		bs,
		grInfo,
		scfPtr,
		C.int(regionLimit),
	)
}


