package adpcm

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/godexture/codec-pcm/internal/adpcm/bits"
	imaadpcm "github.com/godexture/codec-pcm/internal/adpcm/ima"
	msadpcm "github.com/godexture/codec-pcm/internal/adpcm/ms"
	"github.com/godexture/core/domain/media"
)

func TestADPCMRoundtrip(t *testing.T) {
	tests := []struct {
		name     string
		codec    media.CodecID
		channels int
	}{
		{"MS ADPCM Mono", media.CodecMSADPCM, 1},
		{"MS ADPCM Stereo", media.CodecMSADPCM, 2},
		{"IMA ADPCM Mono", media.CodecIMAADPCM, 1},
		{"IMA ADPCM Stereo", media.CodecIMAADPCM, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sampleRate := 8000
			numSamples := sampleRate
			pcm := make([]byte, numSamples*tt.channels*2)
			for i := 0; i < numSamples; i++ {
				val := int16(math.Sin(2.0*math.Pi*100.0*float64(i)/float64(sampleRate)) * 16384.0)
				for c := 0; c < tt.channels; c++ {
					binary.LittleEndian.PutUint16(pcm[(i*tt.channels+c)*2:(i*tt.channels+c)*2+2], uint16(val))
				}
			}

			var encoded []byte
			var err error
			if tt.codec == media.CodecMSADPCM {
				encoded, err = msadpcm.Encode(pcm, tt.channels, binary.LittleEndian)
			} else {
				state := &imaadpcm.EncodeState{}
				encoded, err = imaadpcm.Encode(pcm, tt.channels, binary.LittleEndian, state)
			}
			if err != nil {
				t.Fatalf("Encode error = %v", err)
			}

			blockAlign := 256 * tt.channels
			var decoded []byte
			for offset := 0; offset+blockAlign <= len(encoded); offset += blockAlign {
				block := encoded[offset : offset+blockAlign]
				var decBlock []byte
				if tt.codec == media.CodecMSADPCM {
					decBlock, err = msadpcm.Decode(block, tt.channels, binary.LittleEndian)
				} else {
					decBlock, err = imaadpcm.Decode(block, tt.channels, binary.LittleEndian)
				}
				if err != nil {
					t.Fatalf("Decode error = %v at offset %d", err, offset)
				}
				decoded = append(decoded, decBlock...)
			}

			origSamples := bits.BytesToS16(pcm, binary.LittleEndian)
			decSamples := bits.BytesToS16(decoded, binary.LittleEndian)

			minLen := len(origSamples)
			if len(decSamples) < minLen {
				minLen = len(decSamples)
			}

			var sumDiff float64
			for i := 0; i < minLen; i++ {
				diff := math.Abs(float64(origSamples[i] - decSamples[i]))
				sumDiff += diff
			}
			mae := sumDiff / float64(minLen)

			t.Logf("MAE: %.2f", mae)
			if mae > 500 {
				t.Errorf("MAE is too high: %.2f, expected < 500", mae)
			}
		})
	}
}
