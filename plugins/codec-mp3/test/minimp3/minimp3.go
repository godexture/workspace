package minimp3

/*
#define MINIMP3_FLOAT_OUTPUT
#include "minimp3.h"
#include <stdlib.h>

void mp3dec_init(mp3dec_t *dec);
int mp3dec_decode_frame(mp3dec_t *dec, const uint8_t *mp3, int mp3_bytes, float *pcm, mp3dec_frame_info_t *info);

int decode_mp3(const uint8_t *buf, int buf_size, float **out_pcm, int *out_samples) {
    mp3dec_t dec;
    mp3dec_init(&dec);

    int allocated = 1152 * 2 * 100;
    float *pcm = malloc(allocated * sizeof(float));
    int total_samples = 0;

    int offset = 0;
    mp3dec_frame_info_t info;
    float frame_pcm[1152 * 2];

    while (offset < buf_size) {
        int samples = mp3dec_decode_frame(&dec, buf + offset, buf_size - offset, frame_pcm, &info);
        if (info.frame_bytes > 0) {
            if (samples > 0) {
                int decoded_samples = samples * info.channels;
                if (total_samples + decoded_samples > allocated) {
                    allocated *= 2;
                    pcm = realloc(pcm, allocated * sizeof(float));
                }
                for (int i = 0; i < decoded_samples; i++) {
                    pcm[total_samples + i] = frame_pcm[i];
                }
                total_samples += decoded_samples;
            }
            offset += info.frame_bytes;
        } else {
            break;
        }
    }

    *out_pcm = pcm;
    *out_samples = total_samples;
    return 0;
}
*/
import "C"
import (
	"unsafe"

	"github.com/godexture/codec-mp3/internal/mp3"
)

func Decode(data []byte) []float32 {
	skipped := mp3.SkipID3(data)
	data = data[skipped:]

	var outPcm *C.float
	var outSamples C.int

	C.decode_mp3((*C.uint8_t)(unsafe.Pointer(&data[0])), C.int(len(data)), &outPcm, &outSamples)

	samples := int(outSamples)
	pcmSlice := unsafe.Slice((*float32)(unsafe.Pointer(outPcm)), samples)
	defer C.free(unsafe.Pointer(outPcm))

	res := make([]float32, samples)
	copy(res, pcmSlice)
	return append([]float32(nil), res...)
}
