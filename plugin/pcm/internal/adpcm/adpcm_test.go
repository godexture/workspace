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

			var encoded []byte
			if tt.kind == param.Microsoft {
				encoded, err = msadpcm.Encode(pcm, tt.channels, params, binary.LittleEndian)
			} else {
				state := &imaadpcm.EncodeState{}
				encoded, err = imaadpcm.Encode(pcm, tt.channels, params, binary.LittleEndian, state)
			}
			if err != nil {
				t.Fatalf("Encode error = %v", err)
			}

			blockAlign := int(params.BlockAlign)
			var decoded []byte
			for offset := 0; offset+blockAlign <= len(encoded); offset += blockAlign {
				block := encoded[offset : offset+blockAlign]
				var decBlock []byte
				if tt.kind == param.Microsoft {
					decBlock, err = msadpcm.Decode(block, tt.channels, params, binary.LittleEndian)
				} else {
					decBlock, err = imaadpcm.Decode(block, tt.channels, params, binary.LittleEndian)
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

	decoded, err := msadpcm.Decode(block, 1, adpcm, binary.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	samples := bits.BytesToS16(decoded, binary.LittleEndian)
	if samples[2] != 0 {
		t.Fatalf("sample decoded with configured coefficients = %d, want 0", samples[2])
	}
}
