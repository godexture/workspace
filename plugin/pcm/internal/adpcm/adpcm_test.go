package adpcm

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/godexture/godec/plugin/pcm/internal/adpcm/bits"
	imaadpcm "github.com/godexture/godec/plugin/pcm/internal/adpcm/ima"
	msadpcm "github.com/godexture/godec/plugin/pcm/internal/adpcm/ms"
	"github.com/godexture/godec/plugin/pcm/internal/adpcm/param"
)

func TestADPCMRoundtrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		kind     param.Kind
		channels int
	}{
		{"MS ADPCM Mono", param.Microsoft, 1},
		{"MS ADPCM Stereo", param.Microsoft, 2},
		{"IMA ADPCM Mono", param.IMA, 1},
		{"IMA ADPCM Stereo", param.IMA, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sampleRate := 8000
			numSamples := sampleRate
			pcm := make([]byte, numSamples*tt.channels*2)
			for i := 0; i < numSamples; i++ {
				val := int16(math.Sin(2.0*math.Pi*100.0*float64(i)/float64(sampleRate)) * 16384.0)
				for c := 0; c < tt.channels; c++ {
					binary.LittleEndian.PutUint16(pcm[(i*tt.channels+c)*2:(i*tt.channels+c)*2+2], uint16(val))
				}
			}
			params, err := param.Default(tt.kind, tt.channels)
			if err != nil {
				t.Fatal(err)
			}

			perBlock := int(params.SamplesPerBlock) * tt.channels
			source := bits.BytesToS16(pcm, binary.LittleEndian)
			state := &imaadpcm.EncodeState{}
			chunk := make([]int16, perBlock)
			encoded := make([]byte, 0, len(source)/perBlock*int(params.BlockAlign))
			block := make([]byte, params.BlockAlign)
			for offset := 0; offset+perBlock <= len(source); offset += perBlock {
				copy(chunk, source[offset:offset+perBlock])
				if tt.kind == param.Microsoft {
					err = msadpcm.EncodeBlock(block, chunk, params, tt.channels)
				} else {
					err = imaadpcm.EncodeBlock(block, chunk, params, tt.channels, state)
				}
				if err != nil {
					t.Fatalf("Encode error = %v", err)
				}
				encoded = append(encoded, block...)
			}

			blockAlign := int(params.BlockAlign)
			planes := make([][]int16, tt.channels)
			for index := range planes {
				planes[index] = make([]int16, params.SamplesPerBlock)
			}
			var decSamples []int16
			for offset := 0; offset+blockAlign <= len(encoded); offset += blockAlign {
				block := encoded[offset : offset+blockAlign]
				if tt.kind == param.Microsoft {
					err = msadpcm.Decode(planes, block, params)
				} else {
					err = imaadpcm.Decode(planes, block, params)
				}
				if err != nil {
					t.Fatalf("Decode error = %v at offset %d", err, offset)
				}
				for position := range int(params.SamplesPerBlock) {
					for _, plane := range planes {
						decSamples = append(decSamples, plane[position])
					}
				}
			}

			origSamples := source

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

func TestMSADPCMDecodeUsesConfiguredCoefficients(t *testing.T) {
	t.Parallel()
	adpcm, err := param.Default(param.Microsoft, 1)
	if err != nil {
		t.Fatal(err)
	}
	adpcm.BlockAlign = 8
	adpcm.SamplesPerBlock, err = param.SamplesPerBlock(param.Microsoft, 1, adpcm.BlockAlign)
	if err != nil {
		t.Fatal(err)
	}
	adpcm.Coefficients = []param.Coefficient{{Coeff1: 0, Coeff2: 0}}
	block := []byte{0, 16, 0, 10, 0, 20, 0, 0}

	samples := make([]int16, adpcm.SamplesPerBlock)
	if err := msadpcm.Decode([][]int16{samples}, block, adpcm); err != nil {
		t.Fatal(err)
	}
	if samples[2] != 0 {
		t.Fatalf("sample decoded with configured coefficients = %d, want 0", samples[2])
	}
}
