package internal

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/godexture/core/domain/media"
)

func wavResolvedAudioFormat(header wavHeader) uint16 {
	if header.audioFormat == wavAudioExtensible && bytes.Equal(header.subFormat[4:], wavSubFormatBase) {
		return binary.LittleEndian.Uint16(header.subFormat[0:2])
	}

	return header.audioFormat
}

func codecFromWAVAudioFormat(audioFormat uint16) (media.CodecID, bool) {
	switch audioFormat {
	case wavAudioPCM, wavAudioIEEEFloat:
		return media.CodecLPCM, true
	case wavAudioALaw:
		return media.CodecPCMA, true
	case wavAudioULaw:
		return media.CodecPCMU, true
	case wavAudioMSADPCM:
		return media.CodecMSADPCM, true
	case wavAudioIMAADPCM:
		return media.CodecIMAADPCM, true
	case wavAudioMP3:
		return media.CodecMP3, true
	case wavAudioGSM:
		return media.CodecGSM, true
	default:
		return "", false
	}
}

func sampleFormatFromHeader(audioFormat, bitsPerSample uint16) (media.SampleFormat, error) {
	switch audioFormat {
	case wavAudioPCM:
		switch bitsPerSample {
		case 8:
			return media.SampleFormatU8, nil
		case 16:
			return media.SampleFormatS16, nil
		case 24:
			return media.SampleFormatS24, nil
		case 32:
			return media.SampleFormatS32, nil
		default:
			return media.SampleFormatUnknown, fmt.Errorf("unsupported pcm bit depth: %d", bitsPerSample)
		}

	case wavAudioIEEEFloat:
		switch bitsPerSample {
		case 32:
			return media.SampleFormatF32, nil
		case 64:
			return media.SampleFormatF64, nil
		default:
			return media.SampleFormatUnknown, fmt.Errorf("unsupported float bit depth: %d", bitsPerSample)
		}

	case wavAudioALaw, wavAudioULaw:
		if bitsPerSample != 8 {
			return media.SampleFormatUnknown, fmt.Errorf("unsupported g711 bit depth: %d", bitsPerSample)
		}
		return media.SampleFormatU8, nil

	case wavAudioMSADPCM, wavAudioIMAADPCM, wavAudioMP3, wavAudioGSM:
		return media.SampleFormatUnknown, nil

	default:
		return media.SampleFormatUnknown, fmt.Errorf("unsupported wav audio format tag: %d", audioFormat)
	}
}

func sampleFormatFromWAV(audioFormat uint16, bitsPerSample uint16) media.SampleFormat {
	format, err := sampleFormatFromHeader(audioFormat, bitsPerSample)
	if err != nil {
		return media.SampleFormatUnknown
	}

	return format
}

func layoutFromChannelCount(channels int) media.ChannelLayout {
	switch channels {
	case 1:
		return media.LayoutMono1
	case 2:
		return media.LayoutStereo2_0
	case 3:
		return media.LayoutStereo3_0
	case 4:
		return media.LayoutQuad4_0
	case 5:
		return media.LayoutFront5_0
	case 6:
		return media.LayoutFront5_1
	case 7:
		return media.LayoutSide7_0
	case 8:
		return media.LayoutSurround7_1
	default:
		return media.NewUnspecified(channels)
	}
}
