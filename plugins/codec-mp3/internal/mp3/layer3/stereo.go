package layer3

import (
	"github.com/godexture/godec/plugins/format-mp3/header"
)

func stereoTopBand(rightChannel []float32, scaleFactorBandTable []byte, bandCount int, maxBand []int) {
	maxBand[0] = -1
	maxBand[1] = -1
	maxBand[2] = -1

	sampleIndex := 0
	for i := 0; i < bandCount; i++ {
		bandWidth := int(scaleFactorBandTable[i])
		for k := 0; k < bandWidth; k += 2 {
			if rightChannel[sampleIndex+k] != 0 || rightChannel[sampleIndex+k+1] != 0 {
				maxBand[i%3] = i
				break
			}
		}
		sampleIndex += bandWidth
	}
}

func intensityStereoBand(leftChannel []float32, bandWidth int, ratioLeft float32, ratioRight float32) {
	for i := 0; i < bandWidth; i++ {
		leftChannel[i+SamplesPerGranule] = leftChannel[i] * ratioRight
		leftChannel[i] = leftChannel[i] * ratioLeft
	}
}

func midSideStereo(leftChannel []float32, bandWidth int) {
	for i := 0; i < bandWidth; i++ {
		leftSample := leftChannel[i]
		rightSample := leftChannel[i+SamplesPerGranule]
		leftChannel[i] = leftSample + rightSample
		leftChannel[i+SamplesPerGranule] = leftSample - rightSample
	}
}

var pan = [14]float32{0, 1, 0.21132487, 0.78867513, 0.36602540, 0.63397460, 0.5, 0.5, 0.63397460, 0.36602540, 0.78867513, 0.21132487, 1, 0}

func stereoProcess(leftChannel []float32, intensityStereoPosition []byte, scaleFactorBandTable []byte, h header.Header, maxBand []int, mpeg2Shift int) {
	maxPos := 64
	if h.IsMPEG1() {
		maxPos = 7
	}

	sampleOffset := 0
	for i := 0; ; i++ {
		bandWidth := int(scaleFactorBandTable[i])
		if bandWidth == 0 {
			break
		}
		intensityPosition := int(intensityStereoPosition[i])
		if i > maxBand[i%3] && intensityPosition < maxPos {
			var scaleLeft, scaleRight float32
			var msScaling float32 = 1.0
			if h.IsMidSideStereoEnabled() {
				msScaling = 1.41421356
			}
			if h.IsMPEG1() {
				scaleLeft = pan[2*intensityPosition]
				scaleRight = pan[PanIndexRight(2*intensityPosition)]
			} else {
				scaleLeft = 1.0
				scaleRight = ldexpQ2(1.0, ((intensityPosition+1)>>1)<<mpeg2Shift)
				if (intensityPosition & 1) != 0 {
					scaleLeft = scaleRight
					scaleRight = 1.0
				}
			}
			intensityStereoBand(leftChannel[sampleOffset:], bandWidth, scaleLeft*msScaling, scaleRight*msScaling)
		} else if h.IsMidSideStereoEnabled() {
			midSideStereo(leftChannel[sampleOffset:], bandWidth)
		}
		sampleOffset += bandWidth
	}
}

// PanIndexRight returns the right pan channel index.
func PanIndexRight(leftIndex int) int {
	return leftIndex + 1
}

func IntensityStereo(leftChannel []float32, intensityStereoPosition []byte, granule *GranuleInfo, granule1 *GranuleInfo, h header.Header) {
	var maxBand [3]int
	numScaleFactorBands := int(granule.LongScaleFactorBandCount + granule.ShortScaleFactorBandCount)
	numSubBlocks := 1
	if granule.ShortScaleFactorBandCount != 0 {
		numSubBlocks = 3
	}

	stereoTopBand(leftChannel[SamplesPerGranule:], granule.ScaleFactorBandTable, numScaleFactorBands, maxBand[:])
	if granule.LongScaleFactorBandCount != 0 {
		m := max(max(maxBand[0], maxBand[1]), maxBand[2])
		maxBand[0] = m
		maxBand[1] = m
		maxBand[2] = m
	}
	for i := 0; i < numSubBlocks; i++ {
		defaultIntensityPos := 0
		if h.IsMPEG1() {
			defaultIntensityPos = 3
		}
		subBlockBandIndex := numScaleFactorBands - numSubBlocks + i
		prevBandIndex := subBlockBandIndex - numSubBlocks
		if maxBand[i] >= prevBandIndex {
			intensityStereoPosition[subBlockBandIndex] = byte(defaultIntensityPos)
		} else {
			intensityStereoPosition[subBlockBandIndex] = intensityStereoPosition[prevBandIndex]
		}
	}
	stereoProcess(leftChannel, intensityStereoPosition, granule.ScaleFactorBandTable, h, maxBand[:], int(granule1.ScaleFactorCompression&1))
}
