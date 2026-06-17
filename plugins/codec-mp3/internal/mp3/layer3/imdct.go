package layer3

var imdctWindowTables = [2][18]float32{
	{0.99904822, 0.99144486, 0.97629601, 0.95371695, 0.92387953, 0.88701083, 0.84339145, 0.79335334, 0.73727734, 0.04361938, 0.13052619, 0.21643961, 0.30070580, 0.38268343, 0.46174861, 0.53729961, 0.60876143, 0.67559021},
	{1, 1, 1, 1, 1, 1, 0.99144486, 0.92387953, 0.79335334, 0, 0, 0, 0, 0, 0, 0.13052619, 0.38268343, 0.60876143},
}

var twiddleFactors9 = [18]float32{
	0.73727734, 0.79335334, 0.84339145, 0.88701083, 0.92387953, 0.95371695, 0.97629601, 0.99144486, 0.99904822, 0.67559021, 0.60876143, 0.53729961, 0.46174861, 0.38268343, 0.30070580, 0.21643961, 0.13052619, 0.04361938,
}

func dct39(coefficients []float32) {
	s0 := coefficients[0]
	s2 := coefficients[2]
	s4 := coefficients[4]
	s6 := coefficients[6]
	s8 := coefficients[8]
	t0 := s0 + s6*0.5
	s0 -= s6
	t4 := (s4 + s2) * 0.93969262
	t2 := (s8 + s2) * 0.76604444
	s6 = (s4 - s8) * 0.17364818
	s4 += s8 - s2

	s2 = s0 - s4*0.5
	coefficients[4] = s4 + s0
	s8 = t0 - t2 + s6
	s0 = t0 - t4 + t2
	s4 = t0 + t4 - s6

	s1 := coefficients[1]
	s3 := coefficients[3]
	s5 := coefficients[5]
	s7 := coefficients[7]

	s3 *= 0.86602540
	t0 = (s5 + s1) * 0.98480775
	t4 = (s5 - s7) * 0.34202014
	t2 = (s1 + s7) * 0.64278761
	s1 = (s1 - s5 - s7) * 0.86602540

	s5 = t0 - s3 - t2
	s7 = t4 - s3 - t0
	s3 = t4 + s3 - t2

	coefficients[0] = s4 - s7
	coefficients[1] = s2 + s1
	coefficients[2] = s0 - s3
	coefficients[3] = s8 + s5
	coefficients[5] = s8 - s5
	coefficients[6] = s0 + s3
	coefficients[7] = s2 - s1
	coefficients[8] = s4 + s7
}

func imdct36(granule []float32, overlap []float32, windowTable []float32, bandCount int) {
	for bandIndex := 0; bandIndex < bandCount; bandIndex++ {
		subBandGranule := granule[bandIndex*18 : bandIndex*18+18]
		subBandOverlap := overlap[bandIndex*9 : bandIndex*9+9]

		var cosTerms, sinTerms [9]float32
		cosTerms[0] = -subBandGranule[0]
		sinTerms[0] = subBandGranule[17]
		for i := 0; i < 4; i++ {
			sinTerms[8-2*i] = subBandGranule[4*i+1] - subBandGranule[4*i+2]
			cosTerms[1+2*i] = subBandGranule[4*i+1] + subBandGranule[4*i+2]
			sinTerms[7-2*i] = subBandGranule[4*i+4] - subBandGranule[4*i+3]
			cosTerms[2+2*i] = -(subBandGranule[4*i+3] + subBandGranule[4*i+4])
		}
		dct39(cosTerms[:])
		dct39(sinTerms[:])

		sinTerms[1] = -sinTerms[1]
		sinTerms[3] = -sinTerms[3]
		sinTerms[5] = -sinTerms[5]
		sinTerms[7] = -sinTerms[7]

		for i := 0; i < 9; i++ {
			overlapVal := subBandOverlap[i]
			sum := cosTerms[i]*twiddleFactors9[9+i] + sinTerms[i]*twiddleFactors9[0+i]
			subBandOverlap[i] = cosTerms[i]*twiddleFactors9[0+i] - sinTerms[i]*twiddleFactors9[9+i]
			subBandGranule[i] = overlapVal*windowTable[0+i] - sum*windowTable[9+i]
			subBandGranule[17-i] = overlapVal*windowTable[9+i] + sum*windowTable[0+i]
		}
	}
}

func idct3(in0, in1, in2 float32, dest []float32) {
	m1 := in1 * 0.86602540
	a1 := in0 - in2*0.5
	dest[1] = in0 + in2
	dest[0] = a1 + m1
	dest[2] = a1 - m1
}

func imdct12(inputSamples []float32, inputOffset int, destSamples []float32, overlapBuffer []float32) {
	var twiddleFactors3 = [6]float32{0.79335334, 0.92387953, 0.99144486, 0.60876143, 0.38268343, 0.13052619}
	var cosTerms, sinTerms [3]float32

	idct3(-inputSamples[inputOffset+0], inputSamples[inputOffset+6]+inputSamples[inputOffset+3], inputSamples[inputOffset+12]+inputSamples[inputOffset+9], cosTerms[:])
	idct3(inputSamples[inputOffset+15], inputSamples[inputOffset+12]-inputSamples[inputOffset+9], inputSamples[inputOffset+6]-inputSamples[inputOffset+3], sinTerms[:])
	sinTerms[1] = -sinTerms[1]

	for i := 0; i < 3; i++ {
		overlapVal := overlapBuffer[i]
		sum := cosTerms[i]*twiddleFactors3[3+i] + sinTerms[i]*twiddleFactors3[0+i]
		overlapBuffer[i] = cosTerms[i]*twiddleFactors3[0+i] - sinTerms[i]*twiddleFactors3[3+i]
		destSamples[i] = overlapVal*twiddleFactors3[2-i] - sum*twiddleFactors3[5-i]
		destSamples[5-i] = overlapVal*twiddleFactors3[5-i] + sum*twiddleFactors3[2-i]
	}
}

func imdctShort(granule []float32, overlapBuffer []float32, bandCount int) {
	for bandIndex := 0; bandIndex < bandCount; bandIndex++ {
		subBandGranule := granule[bandIndex*18:]
		subBandOverlap := overlapBuffer[bandIndex*9:]
		var tempGranule [18]float32
		copy(tempGranule[:], subBandGranule[:18])
		copy(subBandGranule[:6], subBandOverlap[:6])
		imdct12(tempGranule[:], 0, subBandGranule[6:], subBandOverlap[6:])
		imdct12(tempGranule[:], 1, subBandGranule[12:], subBandOverlap[6:])
		imdct12(tempGranule[:], 2, subBandOverlap, subBandOverlap[6:])
	}
}

// ImdctGo is the pure Go implementation of IMDCT.
func ImdctGo(granule []float32, overlap []float32, blockType int, longBandCount int) {
	granuleOffset := 0
	overlapOffset := 0
	if longBandCount > 0 {
		imdct36(granule, overlap, imdctWindowTables[0][:], longBandCount)
		granuleOffset += SamplesPerSubBand * longBandCount
		overlapOffset += (SamplesPerSubBand / 2) * longBandCount
	}
	if blockType == 2 { // SHORT_BLOCK_TYPE = 2
		imdctShort(granule[granuleOffset:], overlap[overlapOffset:], SubBandCount-longBandCount)
	} else {
		windowIndex := 0
		if blockType == 3 { // STOP_BLOCK_TYPE = 3
			windowIndex = 1
		}
		imdct36(granule[granuleOffset:], overlap[overlapOffset:], imdctWindowTables[windowIndex][:], SubBandCount-longBandCount)
	}
}

// Imdct performs IMDCT on a granule block.
func Imdct(granule []float32, overlap []float32, blockType int, longBandCount int) {
	ImdctGo(granule, overlap, blockType, longBandCount)
}
