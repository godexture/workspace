//go:build ignore

package main

import (
	"log"

	generator "github.com/godexture/tools/pkg/table-generator"
)

const (
	bias = 0x84
	clip = 32635
)

func ULawToLinear(uVal byte) int16 {
	uVal = ^uVal
	sign := uVal & 0x80
	exponent := (uVal >> 4) & 0x07
	mantissa := uVal & 0x0F
	sample := (int16(mantissa) << 3) + bias
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
		if pcm == -32768 {
			pcm = clip
		} else {
			pcm = -pcm
		}
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
	if sign == 0 {
		return -sample
	}
	return sample
}

func LinearToALaw(pcm int16) byte {
	sign := int16(0x80)
	if pcm < 0 {
		if pcm == -32768 {
			pcm = 32767
		} else {
			pcm = -pcm
		}
		sign = 0
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

func main() {
	var uLawToLinearTable [256]uint16
	for i := 0; i < 256; i++ {
		uLawToLinearTable[i] = uint16(ULawToLinear(byte(i)))
	}

	var aLawToLinearTable [256]uint16
	for i := 0; i < 256; i++ {
		aLawToLinearTable[i] = uint16(ALawToLinear(byte(i)))
	}

	var linearToULawTable [65536]byte
	for i := 0; i <= 65535; i++ {
		linearToULawTable[i] = LinearToULaw(int16(i))
	}

	var linearToALawTable [65536]byte
	for i := 0; i <= 65535; i++ {
		linearToALawTable[i] = LinearToALaw(int16(i))
	}

	tables := []generator.Table{
		{
			Name: "uLawToLinearTable",
			Type: "[256]uint16",
			Data: uLawToLinearTable,
		},
		{
			Name: "aLawToLinearTable",
			Type: "[256]uint16",
			Data: aLawToLinearTable,
		},
		{
			Name: "linearToULawTable",
			Type: "[65536]byte",
			Data: linearToULawTable,
		},
		{
			Name: "linearToALawTable",
			Type: "[65536]byte",
			Data: linearToALawTable,
		},
	}

	if err := generator.Generate("tables.go", "g711", tables); err != nil {
		log.Fatalf("generate failed: %v", err)
	}
}
