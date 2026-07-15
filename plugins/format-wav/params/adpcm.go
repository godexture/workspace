// Package params defines the codec-parameter payload used by the WAV
// format plugin for Microsoft and IMA ADPCM streams.
package params

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/godexture/core/domain/media"
)

// SchemaADPCM identifies the binary representation produced by ADPCM.MarshalBinary.
const SchemaADPCM = "github.com/godexture/format-wav/adpcm"

// Coefficient is one Microsoft ADPCM predictor coefficient pair.
type Coefficient struct {
	Coeff1 int16
	Coeff2 int16
}

// ADPCM contains the framing parameters shared by a WAV ADPCM stream and its codec.
// Coefficients is used only by Microsoft ADPCM.
type ADPCM struct {
	BlockAlign      uint16
	SamplesPerBlock uint16
	Coefficients    []Coefficient
}

var standardMSCoefficients = []Coefficient{
	{256, 0}, {512, -256}, {0, 0}, {192, 64}, {240, 0}, {460, -208}, {392, -232},
}

// Default returns the compatibility default used for newly encoded ADPCM streams.
func Default(codec media.CodecID, channels int) (ADPCM, error) {
	if channels != 1 && channels != 2 {
		return ADPCM{}, fmt.Errorf("unsupported channel count for ADPCM: %d", channels)
	}
	p := ADPCM{BlockAlign: uint16(256 * channels)}
	if codec == media.CodecMSADPCM {
		p.Coefficients = append([]Coefficient(nil), standardMSCoefficients...)
	}
	spb, err := SamplesPerBlock(codec, channels, p.BlockAlign)
	if err != nil {
		return ADPCM{}, err
	}
	p.SamplesPerBlock = spb
	return p, nil
}

// SamplesPerBlock derives the WAV ADPCM samples-per-block value from block alignment.
func SamplesPerBlock(codec media.CodecID, channels int, blockAlign uint16) (uint16, error) {
	b := int(blockAlign)
	var samples int
	switch codec {
	case media.CodecMSADPCM:
		switch channels {
		case 1:
			if b < 7 {
				return 0, errors.New("MS ADPCM mono block align is too small")
			}
			samples = (b-7)*2 + 2
		case 2:
			if b < 14 {
				return 0, errors.New("MS ADPCM stereo block align is too small")
			}
			samples = b - 12
		default:
			return 0, fmt.Errorf("unsupported channel count for MS ADPCM: %d", channels)
		}
	case media.CodecIMAADPCM:
		switch channels {
		case 1:
			if b < 4 {
				return 0, errors.New("IMA ADPCM mono block align is too small")
			}
			samples = (b-4)*2 + 1
		case 2:
			if b < 8 || (b-8)%8 != 0 {
				return 0, errors.New("IMA ADPCM stereo block align must be 8 plus a multiple of 8")
			}
			samples = b - 7
		default:
			return 0, fmt.Errorf("unsupported channel count for IMA ADPCM: %d", channels)
		}
	default:
		return 0, fmt.Errorf("unsupported ADPCM codec: %s", codec)
	}
	if samples > int(^uint16(0)) {
		return 0, errors.New("ADPCM samples per block overflows uint16")
	}
	return uint16(samples), nil
}

// Validate verifies parameters against the selected ADPCM codec and channel count.
func (p ADPCM) Validate(codec media.CodecID, channels int) error {
	expected, err := SamplesPerBlock(codec, channels, p.BlockAlign)
	if err != nil {
		return err
	}
	if p.SamplesPerBlock != expected {
		return fmt.Errorf("ADPCM samples per block mismatch: got %d, want %d", p.SamplesPerBlock, expected)
	}
	if codec == media.CodecMSADPCM {
		if len(p.Coefficients) == 0 || len(p.Coefficients) > 256 {
			return fmt.Errorf("MS ADPCM coefficient count must be between 1 and 256: %d", len(p.Coefficients))
		}
	} else if len(p.Coefficients) != 0 {
		return errors.New("IMA ADPCM must not contain MS ADPCM coefficients")
	}
	return nil
}

// MarshalBinary encodes an ADPCM parameter payload for SchemaADPCMV1.
func (p ADPCM) MarshalBinary() []byte {
	out := make([]byte, 6+len(p.Coefficients)*4)
	binary.LittleEndian.PutUint16(out[0:2], p.BlockAlign)
	binary.LittleEndian.PutUint16(out[2:4], p.SamplesPerBlock)
	binary.LittleEndian.PutUint16(out[4:6], uint16(len(p.Coefficients)))
	for i, c := range p.Coefficients {
		offset := 6 + i*4
		binary.LittleEndian.PutUint16(out[offset:offset+2], uint16(c.Coeff1))
		binary.LittleEndian.PutUint16(out[offset+2:offset+4], uint16(c.Coeff2))
	}
	return out
}

// Parse decodes and validates a SchemaADPCMV1 payload.
func Parse(codec media.CodecID, channels int, data []byte) (ADPCM, error) {
	if len(data) < 6 {
		return ADPCM{}, errors.New("ADPCM parameter payload is too small")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if len(data) != 6+count*4 {
		return ADPCM{}, errors.New("ADPCM parameter payload has an invalid coefficient length")
	}
	p := ADPCM{
		BlockAlign:      binary.LittleEndian.Uint16(data[0:2]),
		SamplesPerBlock: binary.LittleEndian.Uint16(data[2:4]),
		Coefficients:    make([]Coefficient, count),
	}
	for i := range p.Coefficients {
		offset := 6 + i*4
		p.Coefficients[i] = Coefficient{
			Coeff1: int16(binary.LittleEndian.Uint16(data[offset : offset+2])),
			Coeff2: int16(binary.LittleEndian.Uint16(data[offset+2 : offset+4])),
		}
	}
	if err := p.Validate(codec, channels); err != nil {
		return ADPCM{}, err
	}
	return p, nil
}
