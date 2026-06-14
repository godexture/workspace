package mp3

/*
#cgo CFLAGS: -O3 -DMINIMP3_FLOAT_OUTPUT
#include "minimp3.h"

int mp3dec_skip_id3_bytes(const unsigned char *buf, int size);
void mp3dec_synth_granule_c(float *qmf_state, float *grbuf, int nbands, int nch, float *pcm, float *lins);
*/
import "C"
import (
	"unsafe"
)

// Mp3Dec is the Go wrapper for mp3dec_t.
type Mp3Dec struct {
	cDec C.mp3dec_t
}

// Mp3DecFrameInfo is the Go wrapper for mp3dec_frame_info_t.
type Mp3DecFrameInfo struct {
	FrameBytes  int
	FrameOffset int
	Channels    int
	Hz          int
	Layer       int
	BitrateKbps int
}

// Init initializes the decoder.
func (dec *Mp3Dec) Init() {
	C.mp3dec_init(&dec.cDec)
}

// DecodeFrame decodes one MP3 frame to float32 samples.
// pcm slice must be pre-allocated to hold up to MINIMP3_MAX_SAMPLES_PER_FRAME (1152*2) samples.
// Returns the number of samples decoded *per channel*, and the frame info.
func (dec *Mp3Dec) DecodeFrame(mp3 []byte, pcm []float32) (int, Mp3DecFrameInfo) {
	var cInfo C.mp3dec_frame_info_t

	var mp3Ptr *C.uint8_t
	if len(mp3) > 0 {
		mp3Ptr = (*C.uint8_t)(unsafe.Pointer(&mp3[0]))
	}

	var pcmPtr *C.float
	if len(pcm) > 0 {
		pcmPtr = (*C.float)(unsafe.Pointer(&pcm[0]))
	}

	samples := int(C.mp3dec_decode_frame(
		&dec.cDec,
		mp3Ptr,
		C.int(len(mp3)),
		pcmPtr,
		&cInfo,
	))

	info := Mp3DecFrameInfo{
		FrameBytes:  int(cInfo.frame_bytes),
		FrameOffset: int(cInfo.frame_offset),
		Channels:    int(cInfo.channels),
		Hz:          int(cInfo.hz),
		Layer:       int(cInfo.layer),
		BitrateKbps: int(cInfo.bitrate_kbps),
	}

	return samples, info
}

// C_synth_granule is a test helper that calls the C mp3d_synth_granule implementation.
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
