package internal

import "encoding/binary"

const (
	bias = 0x84
	clip = 32635
)

func ULawToLinear(uVal byte) int16 {
	uVal = ^uVal
	sign := uVal & 0x80
	exponent := (uVal >> 4) & 0x07
	mantissa := uVal & 0x0F
	sample := (int16(mantissa) << 3) + 0x84
	sample <<= exponent
	sample -= bias
	if sign != 0 {
		return -sample
	}
	return sample
}

func LinearToULaw(pcm int16) byte {
	sign := int16(0)
	if pcm < 0 {
		pcm = -pcm
		sign = 0x80
	}
	if pcm > clip {
		pcm = clip
	}
	pcm += bias

	exponent := int16(7)
	for expMask := int16(0x4000); (pcm&expMask) == 0 && exponent > 0; exponent-- {
		expMask >>= 1
	}
	mantissa := (pcm >> (exponent + 3)) & 0x0F
	uVal := byte(sign | (exponent << 4) | mantissa)
	return ^uVal
}

func ALawToLinear(aVal byte) int16 {
	aVal ^= 0x55
	sign := aVal & 0x80
	exponent := (aVal >> 4) & 0x07
	mantissa := aVal & 0x0F

	var sample int16
	if exponent == 0 {
		sample = (int16(mantissa) << 4) + 0x08
	} else {
		sample = (int16(mantissa) << 4) + 0x108
		sample <<= (exponent - 1)
	}

	if sign != 0 {
		return -sample
	}
	return sample
}

func LinearToALaw(pcm int16) byte {
	sign := int16(0)
	if pcm < 0 {
		pcm = -pcm
		sign = 0x80
	}
	if pcm > 32767 {
		pcm = 32767
	}

	var exponent int16
	var mantissa int16

	if pcm >= 256 {
		exponent = 7
		for expMask := int16(0x4000); (pcm&expMask) == 0 && exponent > 1; exponent-- {
			expMask >>= 1
		}
		mantissa = (pcm >> (exponent + 3)) & 0x0F
	} else {
		exponent = 0
		mantissa = (pcm >> 4) & 0x0F
	}

	aVal := byte(sign | (exponent << 4) | mantissa)
	return aVal ^ 0x55
}

func DecodePCMU(data []byte) []byte {
	out := make([]byte, len(data)*2)
	for i := 0; i < len(data); i++ {
		val := ULawToLinear(data[i])
		binary.LittleEndian.PutUint16(out[i*2:], uint16(val))
	}
	return out
}

func DecodePCMA(data []byte) []byte {
	out := make([]byte, len(data)*2)
	for i := 0; i < len(data); i++ {
		val := ALawToLinear(data[i])
		binary.LittleEndian.PutUint16(out[i*2:], uint16(val))
	}
	return out
}

func EncodePCMU(data []byte) []byte {
	out := make([]byte, len(data)/2)
	for i := 0; i < len(out); i++ {
		val := int16(binary.LittleEndian.Uint16(data[i*2:]))
		out[i] = LinearToULaw(val)
	}
	return out
}

func EncodePCMA(data []byte) []byte {
	out := make([]byte, len(data)/2)
	for i := 0; i < len(out); i++ {
		val := int16(binary.LittleEndian.Uint16(data[i*2:]))
		out[i] = LinearToALaw(val)
	}
	return out
}
