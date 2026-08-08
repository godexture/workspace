// Package param defines codec-local ADPCM block parameters independently of
// any container or media-domain contract.
package param

import (
	"errors"
	"fmt"
)

type Kind uint8

const (
	Microsoft Kind = iota + 1
	IMA
)

func (k Kind) String() string {
	switch k {
	case Microsoft:
		return "Microsoft ADPCM"
	case IMA:
		return "IMA ADPCM"
	default:
		return "unknown ADPCM"
	}
}

type Coefficient struct {
	Coeff1 int16
	Coeff2 int16
}

type Parameters struct {
	BlockAlign      uint16
	SamplesPerBlock uint16
	Coefficients    []Coefficient
}

var standardMicrosoftCoefficients = [...]Coefficient{
	{256, 0}, {512, -256}, {0, 0}, {192, 64}, {240, 0}, {460, -208}, {392, -232},
}

func Default(kind Kind, channels int) (Parameters, error) {
	if channels != 1 && channels != 2 {
		return Parameters{}, fmt.Errorf("unsupported channel count for ADPCM: %d", channels)
	}
	result := Parameters{BlockAlign: uint16(256 * channels)}
	if kind == Microsoft {
		result.Coefficients = append([]Coefficient(nil), standardMicrosoftCoefficients[:]...)
	}
	samples, err := SamplesPerBlock(kind, channels, result.BlockAlign)
	if err != nil {
		return Parameters{}, err
	}
	result.SamplesPerBlock = samples
	return result, nil
}

func SamplesPerBlock(kind Kind, channels int, blockAlign uint16) (uint16, error) {
	size := int(blockAlign)
	var samples int
	switch kind {
	case Microsoft:
		switch channels {
		case 1:
			if size < 7 {
				return 0, errors.New("MS ADPCM mono block align is too small")
			}
			samples = (size-7)*2 + 2
		case 2:
			if size < 14 {
				return 0, errors.New("MS ADPCM stereo block align is too small")
			}
			samples = size - 12
		default:
			return 0, fmt.Errorf("unsupported channel count for MS ADPCM: %d", channels)
		}
	case IMA:
		switch channels {
		case 1:
			if size < 4 {
				return 0, errors.New("IMA ADPCM mono block align is too small")
			}
			samples = (size-4)*2 + 1
		case 2:
			if size < 8 || (size-8)%8 != 0 {
				return 0, errors.New("IMA ADPCM stereo block align must be 8 plus a multiple of 8")
			}
			samples = size - 7
		default:
			return 0, fmt.Errorf("unsupported channel count for IMA ADPCM: %d", channels)
		}
	default:
		return 0, fmt.Errorf("unsupported ADPCM kind: %s", kind)
	}
	if samples > int(^uint16(0)) {
		return 0, errors.New("ADPCM samples per block overflows uint16")
	}
	return uint16(samples), nil
}

func (p Parameters) Validate(kind Kind, channels int) error {
	expected, err := SamplesPerBlock(kind, channels, p.BlockAlign)
	if err != nil {
		return err
	}
	if p.SamplesPerBlock != expected {
		return fmt.Errorf("ADPCM samples per block mismatch: got %d, want %d", p.SamplesPerBlock, expected)
	}
	if kind == Microsoft {
		if len(p.Coefficients) == 0 || len(p.Coefficients) > 256 {
			return fmt.Errorf("MS ADPCM coefficient count must be between 1 and 256: %d", len(p.Coefficients))
		}
	} else if len(p.Coefficients) != 0 {
		return errors.New("IMA ADPCM must not contain MS ADPCM coefficients")
	}
	return nil
}
