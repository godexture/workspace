package mp3

var mdctWindow = [2][18]float32{
	{0.99904822, 0.99144486, 0.97629601, 0.95371695, 0.92387953, 0.88701083, 0.84339145, 0.79335334, 0.73727734, 0.04361938, 0.13052619, 0.21643961, 0.30070580, 0.38268343, 0.46174861, 0.53729961, 0.60876143, 0.67559021},
	{1, 1, 1, 1, 1, 1, 0.99144486, 0.92387953, 0.79335334, 0, 0, 0, 0, 0, 0, 0.13052619, 0.38268343, 0.60876143},
}

var twid9 = [18]float32{
	0.73727734, 0.79335334, 0.84339145, 0.88701083, 0.92387953, 0.95371695, 0.97629601, 0.99144486, 0.99904822, 0.67559021, 0.60876143, 0.53729961, 0.46174861, 0.38268343, 0.30070580, 0.21643961, 0.13052619, 0.04361938,
}

func layer3DiscreteCosineTransform39(y []float32) {
	s0 := y[0]
	s2 := y[2]
	s4 := y[4]
	s6 := y[6]
	s8 := y[8]
	t0 := s0 + s6*0.5
	s0 -= s6
	t4 := (s4 + s2) * 0.93969262
	t2 := (s8 + s2) * 0.76604444
	s6 = (s4 - s8) * 0.17364818
	s4 += s8 - s2

	s2 = s0 - s4*0.5
	y[4] = s4 + s0
	s8 = t0 - t2 + s6
	s0 = t0 - t4 + t2
	s4 = t0 + t4 - s6

	s1 := y[1]
	s3 := y[3]
	s5 := y[5]
	s7 := y[7]

	s3 *= 0.86602540
	t0 = (s5 + s1) * 0.98480775
	t4 = (s5 - s7) * 0.34202014
	t2 = (s1 + s7) * 0.64278761
	s1 = (s1 - s5 - s7) * 0.86602540

	s5 = t0 - s3 - t2
	s7 = t4 - s3 - t0
	s3 = t4 + s3 - t2

	y[0] = s4 - s7
	y[1] = s2 + s1
	y[2] = s0 - s3
	y[3] = s8 + s5
	y[5] = s8 - s5
	y[6] = s0 + s3
	y[7] = s2 - s1
	y[8] = s4 + s7
}

func l3Imdct36(granuleBuffer []float32, overlapBuffer []float32, windowTable []float32, numberOfBands int) {
	for j := 0; j < numberOfBands; j++ {
		currentGranule := granuleBuffer[j*18 : j*18+18]
		currentOverlap := overlapBuffer[j*9 : j*9+9]

		var cos, sin [9]float32
		cos[0] = -currentGranule[0]
		sin[0] = currentGranule[17]
		for i := 0; i < 4; i++ {
			sin[8-2*i] = currentGranule[4*i+1] - currentGranule[4*i+2]
			cos[1+2*i] = currentGranule[4*i+1] + currentGranule[4*i+2]
			sin[7-2*i] = currentGranule[4*i+4] - currentGranule[4*i+3]
			cos[2+2*i] = -(currentGranule[4*i+3] + currentGranule[4*i+4])
		}
		layer3DiscreteCosineTransform39(cos[:])
		layer3DiscreteCosineTransform39(sin[:])

		sin[1] = -sin[1]
		sin[3] = -sin[3]
		sin[5] = -sin[5]
		sin[7] = -sin[7]

		for i := 0; i < 9; i++ {
			overlapValue := currentOverlap[i]
			sum := cos[i]*twid9[9+i] + sin[i]*twid9[0+i]
			currentOverlap[i] = cos[i]*twid9[0+i] - sin[i]*twid9[9+i]
			currentGranule[i] = overlapValue*windowTable[0+i] - sum*windowTable[9+i]
			currentGranule[17-i] = overlapValue*windowTable[9+i] + sum*windowTable[0+i]
		}
	}
}

func l3Idct3(x0, x1, x2 float32, destinationSamples []float32) {
	m1 := x1 * 0.86602540
	a1 := x0 - x2*0.5
	destinationSamples[1] = x0 + x2
	destinationSamples[0] = a1 + m1
	destinationSamples[2] = a1 - m1
}

func l3Imdct12(inputSamples []float32, inputOffset int, destinationSamples []float32, overlapBuffer []float32) {
	var twiddleFactors3 = [6]float32{0.79335334, 0.92387953, 0.99144486, 0.60876143, 0.38268343, 0.13052619}
	var cos, sin [3]float32

	l3Idct3(-inputSamples[inputOffset+0], inputSamples[inputOffset+6]+inputSamples[inputOffset+3], inputSamples[inputOffset+12]+inputSamples[inputOffset+9], cos[:])
	l3Idct3(inputSamples[inputOffset+15], inputSamples[inputOffset+12]-inputSamples[inputOffset+9], inputSamples[inputOffset+6]-inputSamples[inputOffset+3], sin[:])
	sin[1] = -sin[1]

	for i := 0; i < 3; i++ {
		overlapValue := overlapBuffer[i]
		sum := cos[i]*twiddleFactors3[3+i] + sin[i]*twiddleFactors3[0+i]
		overlapBuffer[i] = cos[i]*twiddleFactors3[0+i] - sin[i]*twiddleFactors3[3+i]
		destinationSamples[i] = overlapValue*twiddleFactors3[2-i] - sum*twiddleFactors3[5-i]
		destinationSamples[5-i] = overlapValue*twiddleFactors3[5-i] + sum*twiddleFactors3[2-i]
	}
}

func l3ImdctShort(granuleBuffer []float32, overlapBuffer []float32, numberOfBands int) {
	for j := 0; j < numberOfBands; j++ {
		currentGranule := granuleBuffer[j*18:]
		currentOverlap := overlapBuffer[j*9:]
		var temporaryBuffer [18]float32
		copy(temporaryBuffer[:], currentGranule[:18])
		copy(currentGranule[:6], currentOverlap[:6])
		l3Imdct12(temporaryBuffer[:], 0, currentGranule[6:], currentOverlap[6:])
		l3Imdct12(temporaryBuffer[:], 1, currentGranule[12:], currentOverlap[6:])
		l3Imdct12(temporaryBuffer[:], 2, currentOverlap, currentOverlap[6:])
	}
}

// L3ImdctGo is the pure Go implementation of IMDCT.
func L3ImdctGo(granuleBuffer []float32, overlapBuffer []float32, blockType int, numberOfLongBands int) {
	granuleBufferOffset := 0
	overlapOffset := 0
	if numberOfLongBands > 0 {
		l3Imdct36(granuleBuffer, overlapBuffer, mdctWindow[0][:], numberOfLongBands)
		granuleBufferOffset += SamplesPerSubbandLayer3 * numberOfLongBands
		overlapOffset += (SamplesPerSubbandLayer3 / 2) * numberOfLongBands
	}
	if blockType == 2 { // SHORT_BLOCK_TYPE = 2
		l3ImdctShort(granuleBuffer[granuleBufferOffset:], overlapBuffer[overlapOffset:], NumSubbands-numberOfLongBands)
	} else {
		isStopBlock := 0
		if blockType == 3 { // STOP_BLOCK_TYPE = 3
			isStopBlock = 1
		}
		l3Imdct36(granuleBuffer[granuleBufferOffset:], overlapBuffer[overlapOffset:], mdctWindow[isStopBlock][:], NumSubbands-numberOfLongBands)
	}
}

// L3Imdct performs IMDCT on a granule block.
func L3Imdct(granuleBuffer []float32, overlapBuffer []float32, blockType int, numberOfLongBands int) {
	L3ImdctGo(granuleBuffer, overlapBuffer, blockType, numberOfLongBands)
}
