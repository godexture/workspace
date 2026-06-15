package mp3

// ConvertFloat32ToSigned16BitPCMSamples converts float32 PCM samples to int16 PCM samples.
func ConvertFloat32ToSigned16BitPCMSamples(float32PCMSamples []float32, int16PCMSamples []int16) {
	if len(float32PCMSamples) == 0 || len(int16PCMSamples) == 0 {
		return
	}
	n := len(float32PCMSamples)
	if len(int16PCMSamples) < n {
		n = len(int16PCMSamples)
	}
	for i := 0; i < n; i++ {
		sample := float32PCMSamples[i] * 32768.0
		if sample >= 32766.5 {
			int16PCMSamples[i] = 32767
		} else if sample <= -32767.5 {
			int16PCMSamples[i] = -32768
		} else {
			var s int16
			if sample >= 0 {
				s = int16(sample + 0.5)
			} else {
				s = int16(sample - 0.5)
			}
			int16PCMSamples[i] = s
		}
	}
}

func synthesizePair(pcmSamples []float32, channelCount int, zBuffer []float32) {
	accumulator := (zBuffer[14*64] - zBuffer[0]) * 29
	accumulator += (zBuffer[1*64] + zBuffer[13*64]) * 213
	accumulator += (zBuffer[12*64] - zBuffer[2*64]) * 459
	accumulator += (zBuffer[3*64] + zBuffer[11*64]) * 2037
	accumulator += (zBuffer[10*64] - zBuffer[4*64]) * 5153
	accumulator += (zBuffer[5*64] + zBuffer[9*64]) * 6574
	accumulator += (zBuffer[8*64] - zBuffer[6*64]) * 37489
	accumulator += zBuffer[7*64] * 75038
	pcmSamples[0] = accumulator / 32768.0

	zBufferOffset := zBuffer[2:]
	accumulator = zBufferOffset[14*64] * 104
	accumulator += zBufferOffset[12*64] * 1567
	accumulator += zBufferOffset[10*64] * 9727
	accumulator += zBufferOffset[8*64] * 64019
	accumulator += zBufferOffset[6*64] * -9975
	accumulator += zBufferOffset[4*64] * -45
	accumulator += zBufferOffset[2*64] * 146
	accumulator += zBufferOffset[0] * -5
	pcmSamples[16*channelCount] = accumulator / 32768.0
}

func synthesizeFloat(granuleBuffer []float32, pcmSamples []float32, channelCount int, synthesisWorkspace []float32) {
	leftGranule := granuleBuffer
	rightGranule := granuleBuffer
	if channelCount == 2 {
		rightGranule = granuleBuffer[SamplesPerGranuleLayer3:]
	}

	windowTable := [15 * 16]float32{
		-1, 26, -31, 208, 218, 401, -519, 2063, 2000, 4788, -5517, 7134, 5959, 35640, -39336, 74992,
		-1, 24, -35, 202, 222, 347, -581, 2080, 1952, 4425, -5879, 7640, 5288, 33791, -41176, 74856,
		-1, 21, -38, 196, 225, 294, -645, 2087, 1893, 4063, -6237, 8092, 4561, 31947, -43006, 74630,
		-1, 19, -41, 190, 227, 244, -711, 2085, 1822, 3705, -6589, 8492, 3776, 30112, -44821, 74313,
		-1, 17, -45, 183, 228, 197, -779, 2075, 1739, 3351, -6935, 8840, 2935, 28289, -46617, 73908,
		-1, 16, -49, 176, 228, 153, -848, 2057, 1644, 3004, -7271, 9139, 2037, 26482, -48390, 73415,
		-2, 14, -53, 169, 227, 111, -919, 2032, 1535, 2663, -7597, 9389, 1082, 24694, -50137, 72835,
		-2, 13, -58, 161, 224, 72, -991, 2001, 1414, 2330, -7910, 9592, 70, 22929, -51853, 72169,
		-2, 11, -63, 154, 221, 36, -1064, 1962, 1280, 2006, -8209, 9750, -998, 21189, -53534, 71420,
		-2, 10, -68, 147, 215, 2, -1137, 1919, 1131, 1692, -8491, 9863, -2122, 19478, -55178, 70590,
		-3, 9, -73, 139, 208, -29, -1210, 1870, 970, 1388, -8755, 9935, -3300, 17799, -56778, 69679,
		-3, 8, -79, 132, 200, -57, -1283, 1817, 794, 1095, -8998, 9966, -4533, 16155, -58333, 68692,
		-4, 7, -85, 125, 189, -83, -1356, 1759, 605, 814, -9219, 9959, -5818, 14548, -59838, 67629,
		-4, 7, -91, 117, 177, -106, -1428, 1698, 402, 545, -9416, 9916, -7154, 12980, -61289, 66494,
		-5, 6, -97, 111, 163, -127, -1498, 1634, 185, 288, -9585, 9838, -8540, 11455, -62684, 65290,
	}

	zLineOffset := 15 * 64
	windowIndex := 0

	synthesisWorkspace[zLineOffset+4*15] = leftGranule[18*16]
	synthesisWorkspace[zLineOffset+4*15+1] = rightGranule[18*16]
	synthesisWorkspace[zLineOffset+4*15+2] = leftGranule[0]
	synthesisWorkspace[zLineOffset+4*15+3] = rightGranule[0]

	synthesisWorkspace[zLineOffset+4*31] = leftGranule[1+18*16]
	synthesisWorkspace[zLineOffset+4*31+1] = rightGranule[1+18*16]
	synthesisWorkspace[zLineOffset+4*31+2] = leftGranule[1]
	synthesisWorkspace[zLineOffset+4*31+3] = rightGranule[1]

	synthesizePair(pcmSamples[channelCount-1:], channelCount, synthesisWorkspace[4*15+1:])
	synthesizePair(pcmSamples[32*channelCount+channelCount-1:], channelCount, synthesisWorkspace[4*15+64+1:])
	synthesizePair(pcmSamples, channelCount, synthesisWorkspace[4*15:])
	synthesizePair(pcmSamples[32*channelCount:], channelCount, synthesisWorkspace[4*15+64:])

	for i := 14; i >= 0; i-- {
		synthesisWorkspace[zLineOffset+4*i] = leftGranule[18*(31-i)]
		synthesisWorkspace[zLineOffset+4*i+1] = rightGranule[18*(31-i)]
		synthesisWorkspace[zLineOffset+4*i+2] = leftGranule[1+18*(31-i)]
		synthesisWorkspace[zLineOffset+4*i+3] = rightGranule[1+18*(31-i)]

		synthesisWorkspace[zLineOffset+4*(i+16)] = leftGranule[1+18*(1+i)]
		synthesisWorkspace[zLineOffset+4*(i+16)+1] = rightGranule[1+18*(1+i)]
		synthesisWorkspace[zLineOffset+4*(i-16)+2] = leftGranule[18*(1+i)]
		synthesisWorkspace[zLineOffset+4*(i-16)+3] = rightGranule[18*(1+i)]

		load := func(k int) (float32, float32, int, int) {
			w0 := windowTable[windowIndex]
			windowIndex++
			w1 := windowTable[windowIndex]
			windowIndex++
			vZeroIndex := zLineOffset + 4*i - k*64
			vYIndex := zLineOffset + 4*i - (15-k)*64
			return w0, w1, vZeroIndex, vYIndex
		}

		var a, b [4]float32

		// S0(0)
		{
			w0, w1, vZeroIndex, vYIndex := load(0)
			for j := 0; j < 4; j++ {
				b[j] = synthesisWorkspace[vZeroIndex+j]*w1 + synthesisWorkspace[vYIndex+j]*w0
				a[j] = synthesisWorkspace[vZeroIndex+j]*w0 - synthesisWorkspace[vYIndex+j]*w1
			}
		}
		// S2(1)
		{
			w0, w1, vZeroIndex, vYIndex := load(1)
			for j := 0; j < 4; j++ {
				b[j] += synthesisWorkspace[vZeroIndex+j]*w1 + synthesisWorkspace[vYIndex+j]*w0
				a[j] += synthesisWorkspace[vYIndex+j]*w1 - synthesisWorkspace[vZeroIndex+j]*w0
			}
		}
		// S1(2)
		{
			w0, w1, vZeroIndex, vYIndex := load(2)
			for j := 0; j < 4; j++ {
				b[j] += synthesisWorkspace[vZeroIndex+j]*w1 + synthesisWorkspace[vYIndex+j]*w0
				a[j] += synthesisWorkspace[vZeroIndex+j]*w0 - synthesisWorkspace[vYIndex+j]*w1
			}
		}
		// S2(3)
		{
			w0, w1, vZeroIndex, vYIndex := load(3)
			for j := 0; j < 4; j++ {
				b[j] += synthesisWorkspace[vZeroIndex+j]*w1 + synthesisWorkspace[vYIndex+j]*w0
				a[j] += synthesisWorkspace[vYIndex+j]*w1 - synthesisWorkspace[vZeroIndex+j]*w0
			}
		}
		// S1(4)
		{
			w0, w1, vZeroIndex, vYIndex := load(4)
			for j := 0; j < 4; j++ {
				b[j] += synthesisWorkspace[vZeroIndex+j]*w1 + synthesisWorkspace[vYIndex+j]*w0
				a[j] += synthesisWorkspace[vZeroIndex+j]*w0 - synthesisWorkspace[vYIndex+j]*w1
			}
		}
		// S2(5)
		{
			w0, w1, vZeroIndex, vYIndex := load(5)
			for j := 0; j < 4; j++ {
				b[j] += synthesisWorkspace[vZeroIndex+j]*w1 + synthesisWorkspace[vYIndex+j]*w0
				a[j] += synthesisWorkspace[vYIndex+j]*w1 - synthesisWorkspace[vZeroIndex+j]*w0
			}
		}
		// S1(6)
		{
			w0, w1, vZeroIndex, vYIndex := load(6)
			for j := 0; j < 4; j++ {
				b[j] += synthesisWorkspace[vZeroIndex+j]*w1 + synthesisWorkspace[vYIndex+j]*w0
				a[j] += synthesisWorkspace[vZeroIndex+j]*w0 - synthesisWorkspace[vYIndex+j]*w1
			}
		}
		// S2(7)
		{
			w0, w1, vZeroIndex, vYIndex := load(7)
			for j := 0; j < 4; j++ {
				b[j] += synthesisWorkspace[vZeroIndex+j]*w1 + synthesisWorkspace[vYIndex+j]*w0
				a[j] += synthesisWorkspace[vYIndex+j]*w1 - synthesisWorkspace[vZeroIndex+j]*w0
			}
		}

		if channelCount == 2 {
			pcmSamples[(15-i)*2+1] = a[1] / 32768.0
			pcmSamples[(17+i)*2+1] = b[1] / 32768.0
			pcmSamples[(47-i)*2+1] = a[3] / 32768.0
			pcmSamples[(49+i)*2+1] = b[3] / 32768.0
		}
		pcmSamples[(15-i)*channelCount] = a[0] / 32768.0
		pcmSamples[(17+i)*channelCount] = b[0] / 32768.0
		pcmSamples[(47-i)*channelCount] = a[2] / 32768.0
		pcmSamples[(49+i)*channelCount] = b[2] / 32768.0
	}
}

func discreteCosineTransformType2(granuleBuffer []float32, bandCount int) {
	cosineCoefficients := [24]float32{
		10.19000816, 0.50060302, 0.50241929, 3.40760851, 0.50547093, 0.52249861, 2.05778098, 0.51544732, 0.56694406, 1.48416460, 0.53104258, 0.64682180, 1.16943991, 0.55310392, 0.78815460, 0.97256821, 0.58293498, 1.06067765, 0.83934963, 0.62250412, 1.72244716, 0.74453628, 0.67480832, 5.10114861,
	}

	for k := 0; k < bandCount; k++ {
		var temporaryBuffer [4][8]float32
		yIndex := k

		for i := 0; i < 8; i++ {
			x0 := granuleBuffer[yIndex+i*18]
			x1 := granuleBuffer[yIndex+(15-i)*18]
			x2 := granuleBuffer[yIndex+(16+i)*18]
			x3 := granuleBuffer[yIndex+(31-i)*18]
			t0 := x0 + x3
			t1 := x1 + x2
			t2 := (x1 - x2) * cosineCoefficients[3*i+0]
			t3 := (x0 - x3) * cosineCoefficients[3*i+1]
			temporaryBuffer[0][i] = t0 + t1
			temporaryBuffer[1][i] = (t0 - t1) * cosineCoefficients[3*i+2]
			temporaryBuffer[2][i] = t3 + t2
			temporaryBuffer[3][i] = (t3 - t2) * cosineCoefficients[3*i+2]
		}
		for i := 0; i < 4; i++ {
			x0 := temporaryBuffer[i][0]
			x1 := temporaryBuffer[i][1]
			x2 := temporaryBuffer[i][2]
			x3 := temporaryBuffer[i][3]
			x4 := temporaryBuffer[i][4]
			x5 := temporaryBuffer[i][5]
			x6 := temporaryBuffer[i][6]
			x7 := temporaryBuffer[i][7]

			xtTemporary := x0 - x7
			x0 += x7
			x7 = x1 - x6
			x1 += x6
			x6 = x2 - x5
			x2 += x5
			x5 = x3 - x4
			x3 += x4
			x4 = x0 - x3
			x0 += x3
			x3 = x1 - x2
			x1 += x2
			temporaryBuffer[i][0] = x0 + x1
			temporaryBuffer[i][4] = (x0 - x1) * 0.70710677
			x5 = x5 + x6
			x6 = (x6 + x7) * 0.70710677
			x7 = x7 + xtTemporary
			x3 = (x3 + x4) * 0.70710677
			x5 -= x7 * 0.198912367
			x7 += x5 * 0.382683432
			x5 -= x7 * 0.198912367
			x0 = xtTemporary - x6
			xtTemporary += x6
			temporaryBuffer[i][1] = (xtTemporary + x7) * 0.50979561
			temporaryBuffer[i][2] = (x4 + x3) * 0.54119611
			temporaryBuffer[i][3] = (x0 - x5) * 0.60134488
			temporaryBuffer[i][5] = (x0 + x5) * 0.89997619
			temporaryBuffer[i][6] = (x4 - x3) * 1.30656302
			temporaryBuffer[i][7] = (xtTemporary - x7) * 2.56291556
		}
		for i := 0; i < 7; i++ {
			granuleBuffer[yIndex+0*18] = temporaryBuffer[0][i]
			granuleBuffer[yIndex+1*18] = temporaryBuffer[2][i] + temporaryBuffer[3][i] + temporaryBuffer[3][i+1]
			granuleBuffer[yIndex+2*18] = temporaryBuffer[1][i] + temporaryBuffer[1][i+1]
			granuleBuffer[yIndex+3*18] = temporaryBuffer[2][i+1] + temporaryBuffer[3][i] + temporaryBuffer[3][i+1]
			yIndex += 4 * 18
		}
		granuleBuffer[yIndex+0*18] = temporaryBuffer[0][7]
		granuleBuffer[yIndex+1*18] = temporaryBuffer[2][7] + temporaryBuffer[3][7]
		granuleBuffer[yIndex+2*18] = temporaryBuffer[1][7]
		granuleBuffer[yIndex+3*18] = temporaryBuffer[3][7]
	}
}

// SynthesizeGranule is the Go native implementation of subband synthesis filtering.
func SynthesizeGranule(quadratureMirrorFilterState []float32, granuleBuffer []float32, numberOfBands int, channelCount int, pcmSamples []float32, synthesisWorkspace []float32) {
	for i := 0; i < channelCount; i++ {
		discreteCosineTransformType2(granuleBuffer[SamplesPerGranuleLayer3*i:], numberOfBands)
	}

	copy(synthesisWorkspace[:15*64], quadratureMirrorFilterState[:15*64])

	for i := 0; i < numberOfBands; i += 2 {
		synthesizeFloat(granuleBuffer[i:], pcmSamples[32*channelCount*i:], channelCount, synthesisWorkspace[i*64:])
	}

	if channelCount == 1 {
		for i := 0; i < 15*64; i += 2 {
			quadratureMirrorFilterState[i] = synthesisWorkspace[numberOfBands*64+i]
		}
	} else {
		copy(quadratureMirrorFilterState[:15*64], synthesisWorkspace[numberOfBands*64:numberOfBands*64+15*64])
	}
}
