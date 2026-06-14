//go:build cgo_test

package main

/*
#define MINIMP3_FLOAT_OUTPUT
#include "minimp3.h"
#include <stdlib.h>

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
	"fmt"
	"os"
	"path/filepath"
	"unsafe"
)

func main() {
	testFiles := []string{
		"l1-fl4.mp3",
		"l2-fl13.mp3",
		"l3-he_32khz.mp3",
		"l3-hecommon.mp3",
		"l3-nonstandard-id3v2.mp3",
		"l3-sin1k0db.mp3",
	}

	for _, filename := range testFiles {
		mp3Path := filepath.Join("..", "testdata", filename)
		mp3Data, err := os.ReadFile(mp3Path)
		if err != nil {
			panic(fmt.Errorf("failed to read test MP3 file %s: %v", filename, err))
		}

		// skip id3 using the c function isn't strictly necessary if mp3dec_decode_frame skips it,
		// wait, does mp3dec_decode_frame skip id3?
		// Actually the Go implementation does `skipped := mp3.SkipId3(mp3Data); mp3Data = mp3Data[skipped:]`
		// I will just use a simple go logic to skip ID3v2 if it exists.
		offset := 0
		if len(mp3Data) > 10 && string(mp3Data[:3]) == "ID3" {
			size := (int(mp3Data[6]) << 21) | (int(mp3Data[7]) << 14) | (int(mp3Data[8]) << 7) | int(mp3Data[9])
			offset = size + 10
		}
		mp3Data = mp3Data[offset:]

		var outPcm *C.float
		var outSamples C.int

		C.decode_mp3((*C.uint8_t)(unsafe.Pointer(&mp3Data[0])), C.int(len(mp3Data)), &outPcm, &outSamples)

		samples := int(outSamples)
		pcmSlice := unsafe.Slice((*float32)(unsafe.Pointer(outPcm)), samples)

		snapshotPath := filepath.Join("..", "testdata", "snapshots", filename+".snapshot")

		f, err := os.Create(snapshotPath)
		if err != nil {
			panic(err)
		}

		for _, val := range pcmSlice {
			fmt.Fprintf(f, "%g\n", val)
		}
		f.Close()

		C.free(unsafe.Pointer(outPcm))
		fmt.Printf("Generated snapshot for %s (%d samples)\n", filename, samples)
	}
}
