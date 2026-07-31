package internal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/plugins/format-wav/params"
)

func buildWAVHeader(attr media.MediaAttributes, dataSize uint64, trailerSize uint64, forceRF64 bool) ([]byte, error) {
	formatTag, bitsPerSample, err := wavFormatForMediaAttributes(attr)
	if err != nil {
		return nil, err
	}

	layout := attr.Audio.ChannelLayout
	channels := layout.ChannelCount()
	if channels <= 0 {
		return nil, errors.New("wav muxer requires a valid channel layout")
	}

	sampleRate := attr.Audio.SampleRate
	if sampleRate <= 0 {
		return nil, errors.New("wav muxer requires a valid sample rate")
	}

	var adpcmParams params.ADPCM
	var blockAlign int
	samplesPerBlock := 1
	if attr.Codec == media.CodecMSADPCM || attr.Codec == media.CodecIMAADPCM {
		adpcmParams, err = adpcmParametersFromMediaAttributes(attr, channels)
		if err != nil {
			return nil, err
		}
		blockAlign = int(adpcmParams.BlockAlign)
		samplesPerBlock = int(adpcmParams.SamplesPerBlock)
	} else {
		blockAlign = channels * int(bitsPerSample/8)
	}

	var byteRate int
	if attr.Codec == media.CodecMSADPCM || attr.Codec == media.CodecIMAADPCM {
		byteRate = (sampleRate * blockAlign) / samplesPerBlock
	} else {
		byteRate = sampleRate * blockAlign
	}

	isUnknownSize := dataSize == ^uint64(0)

	pad := uint64(0)
	if !isUnknownSize && dataSize%2 == 1 {
		pad = 1
	}

	isADPCM := attr.Codec == media.CodecMSADPCM || attr.Codec == media.CodecIMAADPCM
	useExtensible := !isADPCM && (channels >= 3 || bitsPerSample > 16)
	if !useExtensible {
		defaultLayout := layoutFromChannelCount(channels)
		if layout.Mask() != defaultLayout.Mask() {
			useExtensible = !isADPCM
		}
	}

	fmtSize := uint32(16)
	if useExtensible {
		fmtSize = 40
	} else if attr.Codec == media.CodecMSADPCM {
		fmtSize = uint32(22 + len(adpcmParams.Coefficients)*4)
	} else if attr.Codec == media.CodecIMAADPCM {
		fmtSize = 20
	}

	writeFormatTag := formatTag
	if useExtensible {
		writeFormatTag = wavAudioExtensible
	}

	writeFact := writeFormatTag != wavAudioPCM

	factSize := uint32(0)
	if writeFact {
		factSize = 12
	}

	var riffSize uint64
	if isUnknownSize {
		riffSize = 0xFFFFFFFF
	} else {
		riffSize = uint64(4) + 8 + uint64(fmtSize) + uint64(factSize) + 8 + dataSize + pad + trailerSize
	}

	useRF64 := forceRF64
	if !isUnknownSize && riffSize >= 0xFFFFFFFF {
		useRF64 = true
	}

	ds64TotalSize := uint64(0)
	if useRF64 {
		ds64TotalSize = 36 // 8 bytes header + 28 bytes payload
		if !isUnknownSize {
			riffSize += ds64TotalSize
		}
	}

	var headerBuf bytes.Buffer

	if useRF64 {
		headerBuf.WriteString(wavTagRF64)
		binary.Write(&headerBuf, binary.LittleEndian, uint32(0xFFFFFFFF))
	} else {
		headerBuf.WriteString(wavTagRIFF)
		binary.Write(&headerBuf, binary.LittleEndian, uint32(riffSize))
	}

	headerBuf.WriteString(wavTagWAVE)

	if useRF64 {
		headerBuf.WriteString(wavTagDS64)
		binary.Write(&headerBuf, binary.LittleEndian, uint32(28))
		if isUnknownSize {
			binary.Write(&headerBuf, binary.LittleEndian, uint64(0xFFFFFFFFFFFFFFFF))
			binary.Write(&headerBuf, binary.LittleEndian, uint64(0xFFFFFFFFFFFFFFFF))
			binary.Write(&headerBuf, binary.LittleEndian, uint64(0xFFFFFFFFFFFFFFFF))
		} else {
			binary.Write(&headerBuf, binary.LittleEndian, riffSize)
			binary.Write(&headerBuf, binary.LittleEndian, dataSize)
			numSamples := uint64(0)
			if blockAlign > 0 {
				numSamples = (dataSize / uint64(blockAlign)) * uint64(samplesPerBlock)
			}
			binary.Write(&headerBuf, binary.LittleEndian, numSamples)
		}
		binary.Write(&headerBuf, binary.LittleEndian, uint32(0)) // tableLength
	}

	headerBuf.WriteString(wavTagFmt)
	binary.Write(&headerBuf, binary.LittleEndian, fmtSize)
	binary.Write(&headerBuf, binary.LittleEndian, writeFormatTag)
	binary.Write(&headerBuf, binary.LittleEndian, uint16(channels))
	binary.Write(&headerBuf, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&headerBuf, binary.LittleEndian, uint32(byteRate))
	binary.Write(&headerBuf, binary.LittleEndian, uint16(blockAlign))
	binary.Write(&headerBuf, binary.LittleEndian, bitsPerSample)

	if useExtensible {
		// LPCM samples narrower than their container are left-justified by the
		// pcm encoder, so the significant width goes out as validBitsPerSample.
		validBits := bitsPerSample
		if effectiveBits := attr.Audio.EffectiveBitsPerSample(); (attr.Codec == media.CodecLPCM || attr.Codec == "") &&
			effectiveBits > 0 && effectiveBits < int(bitsPerSample) {
			validBits = uint16(effectiveBits)
		}
		binary.Write(&headerBuf, binary.LittleEndian, uint16(22))            // cbSize
		binary.Write(&headerBuf, binary.LittleEndian, validBits)             // validBitsPerSample
		binary.Write(&headerBuf, binary.LittleEndian, uint32(layout.Mask())) // channelMask

		binary.Write(&headerBuf, binary.LittleEndian, formatTag)
		binary.Write(&headerBuf, binary.LittleEndian, uint16(0))
		headerBuf.Write(wavSubFormatBase)
	} else if attr.Codec == media.CodecMSADPCM {
		cbSize := 4 + len(adpcmParams.Coefficients)*4
		binary.Write(&headerBuf, binary.LittleEndian, uint16(cbSize))
		binary.Write(&headerBuf, binary.LittleEndian, uint16(samplesPerBlock))
		binary.Write(&headerBuf, binary.LittleEndian, uint16(len(adpcmParams.Coefficients)))
		for _, c := range adpcmParams.Coefficients {
			binary.Write(&headerBuf, binary.LittleEndian, c.Coeff1)
			binary.Write(&headerBuf, binary.LittleEndian, c.Coeff2)
		}
	} else if attr.Codec == media.CodecIMAADPCM {
		binary.Write(&headerBuf, binary.LittleEndian, uint16(2)) // cbSize
		binary.Write(&headerBuf, binary.LittleEndian, uint16(samplesPerBlock))
	}

	if writeFact {
		headerBuf.WriteString(wavTagFact)
		binary.Write(&headerBuf, binary.LittleEndian, uint32(4))
		if isUnknownSize {
			binary.Write(&headerBuf, binary.LittleEndian, uint32(0xFFFFFFFF))
		} else {
			numSamples := uint64(0)
			if blockAlign > 0 {
				numSamples = (dataSize / uint64(blockAlign)) * uint64(samplesPerBlock)
			}
			if useRF64 && numSamples > 0xFFFFFFFF {
				binary.Write(&headerBuf, binary.LittleEndian, uint32(0xFFFFFFFF))
			} else {
				binary.Write(&headerBuf, binary.LittleEndian, uint32(numSamples))
			}
		}
	}

	headerBuf.WriteString(wavTagData)
	if useRF64 {
		binary.Write(&headerBuf, binary.LittleEndian, uint32(0xFFFFFFFF))
	} else {
		if isUnknownSize {
			binary.Write(&headerBuf, binary.LittleEndian, uint32(0xFFFFFFFF))
		} else {
			binary.Write(&headerBuf, binary.LittleEndian, uint32(dataSize))
		}
	}

	return headerBuf.Bytes(), nil
}

func adpcmParametersFromMediaAttributes(attr media.MediaAttributes, channels int) (params.ADPCM, error) {
	if attr.Codec != media.CodecMSADPCM && attr.Codec != media.CodecIMAADPCM {
		return params.ADPCM{}, fmt.Errorf("unsupported ADPCM codec: %s", attr.Codec)
	}
	if media.IsCodecParameters[params.ADPCM](attr.CodecParameters) {
		return params.Parse(attr.Codec, channels, attr.CodecParameters.Data)
	}
	return params.Default(attr.Codec, channels)
}

func wavFormatForMediaAttributes(attr media.MediaAttributes) (audioFormat uint16, bitsPerSample uint16, err error) {
	switch attr.Codec {
	case media.CodecPCMA:
		return wavAudioALaw, 8, nil
	case media.CodecPCMU:
		return wavAudioULaw, 8, nil
	case media.CodecMSADPCM:
		return wavAudioMSADPCM, 4, nil
	case media.CodecIMAADPCM:
		return wavAudioIMAADPCM, 4, nil
	case media.CodecLPCM, "":
		format := attr.Audio.Format.Packed()
		if format.IsInteger() {
			return wavAudioPCM, uint16(format.BitsPerSample()), nil
		} else if format.IsFloat() {
			return wavAudioIEEEFloat, uint16(format.BitsPerSample()), nil
		} else {
			return 0, 0, fmt.Errorf("unsupported wav sample format: %s", attr.Audio.Format)
		}
	default:
		return 0, 0, fmt.Errorf("unsupported wav codec: %s", attr.Codec)
	}
}
