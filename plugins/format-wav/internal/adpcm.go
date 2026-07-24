package internal

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/godexture/format-wav/params"
)

func parseADPCMParameters(audioFormat uint16, channels int, blockAlign uint16, extra []byte) (params.ADPCM, error) {
	codec, ok := codecFromWAVAudioFormat(audioFormat)
	if !ok {
		return params.ADPCM{}, fmt.Errorf("unsupported wav ADPCM format tag: %d", audioFormat)
	}

	adpcm, err := params.Default(codec, channels)
	if err != nil {
		return params.ADPCM{}, err
	}
	adpcm.BlockAlign = blockAlign
	adpcm.SamplesPerBlock, err = params.SamplesPerBlock(codec, channels, blockAlign)
	if err != nil {
		return params.ADPCM{}, err
	}

	if len(extra) < 2 {
		return adpcm, nil
	}
	cbSize := int(binary.LittleEndian.Uint16(extra[0:2]))
	if cbSize == 0 {
		return adpcm, nil
	}
	if len(extra) < 2+cbSize {
		return params.ADPCM{}, errors.New("wav ADPCM fmt extension is truncated")
	}

	switch audioFormat {
	case wavAudioMSADPCM:
		if cbSize < 4 {
			return params.ADPCM{}, errors.New("wav MS ADPCM fmt extension is too small")
		}
		adpcm.SamplesPerBlock = binary.LittleEndian.Uint16(extra[2:4])
		coefficientCount := int(binary.LittleEndian.Uint16(extra[4:6]))
		if cbSize != 4+coefficientCount*4 {
			return params.ADPCM{}, errors.New("wav MS ADPCM coefficient table has an invalid size")
		}
		adpcm.Coefficients = make([]params.Coefficient, coefficientCount)
		for i := range adpcm.Coefficients {
			offset := 6 + i*4
			adpcm.Coefficients[i] = params.Coefficient{
				Coeff1: int16(binary.LittleEndian.Uint16(extra[offset : offset+2])),
				Coeff2: int16(binary.LittleEndian.Uint16(extra[offset+2 : offset+4])),
			}
		}
	case wavAudioIMAADPCM:
		if cbSize != 2 {
			return params.ADPCM{}, errors.New("wav IMA ADPCM fmt extension has an invalid size")
		}
		adpcm.SamplesPerBlock = binary.LittleEndian.Uint16(extra[2:4])
	}

	if err := adpcm.Validate(codec, channels); err != nil {
		return params.ADPCM{}, err
	}
	return adpcm, nil
}
