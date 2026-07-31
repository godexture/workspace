package mp3

import "github.com/godexture/codec-mp3/internal/mp3/layer3"

func synthesizePair(samples []float32, channelCount int, zBuffer []float32) {
	accumulator := (zBuffer[14*64] - zBuffer[0]) * 29
	accumulator += (zBuffer[1*64] + zBuffer[13*64]) * 213
	accumulator += (zBuffer[12*64] - zBuffer[2*64]) * 459
	accumulator += (zBuffer[3*64] + zBuffer[11*64]) * 2037
	accumulator += (zBuffer[10*64] - zBuffer[4*64]) * 5153
	accumulator += (zBuffer[5*64] + zBuffer[9*64]) * 6574
	accumulator += (zBuffer[8*64] - zBuffer[6*64]) * 37489
	accumulator += zBuffer[7*64] * 75038
	samples[0] = accumulator / 32768.0

	zBufferOffset := zBuffer[2:]
	accumulator = zBufferOffset[14*64] * 104
	accumulator += zBufferOffset[12*64] * 1567
	accumulator += zBufferOffset[10*64] * 9727
	accumulator += zBufferOffset[8*64] * 64019
	accumulator += zBufferOffset[6*64] * -9975
	accumulator += zBufferOffset[4*64] * -45
	accumulator += zBufferOffset[2*64] * 146
	accumulator += zBufferOffset[0] * -5
	samples[16*channelCount] = accumulator / 32768.0
}

var synthesizeWindowTable = [15 * 16]float32{
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

func synthesizeFloat(granule []float32, samples []float32, channelCount int, workspace []float32) {
	left := granule
	right := granule
	if channelCount == 2 {
		right = granule[layer3.SamplesPerGranule:]
	}

	zLineOffset := 15 * 64
	windowIndex := 0

	workspace[zLineOffset+4*15] = left[18*16]
	workspace[zLineOffset+4*15+1] = right[18*16]
	workspace[zLineOffset+4*15+2] = left[0]
	workspace[zLineOffset+4*15+3] = right[0]

	workspace[zLineOffset+4*31] = left[1+18*16]
	workspace[zLineOffset+4*31+1] = right[1+18*16]
	workspace[zLineOffset+4*31+2] = left[1]
	workspace[zLineOffset+4*31+3] = right[1]

	synthesizePair(samples[channelCount-1:], channelCount, workspace[4*15+1:])
	synthesizePair(samples[32*channelCount+channelCount-1:], channelCount, workspace[4*15+64+1:])
	synthesizePair(samples, channelCount, workspace[4*15:])
	synthesizePair(samples[32*channelCount:], channelCount, workspace[4*15+64:])

	for i := 14; i >= 0; i-- {
		workspace[zLineOffset+4*i] = left[18*(31-i)]
		workspace[zLineOffset+4*i+1] = right[18*(31-i)]
		workspace[zLineOffset+4*i+2] = left[1+18*(31-i)]
		workspace[zLineOffset+4*i+3] = right[1+18*(31-i)]

		workspace[zLineOffset+4*(i+16)] = left[1+18*(1+i)]
		workspace[zLineOffset+4*(i+16)+1] = right[1+18*(1+i)]
		workspace[zLineOffset+4*(i-16)+2] = left[18*(1+i)]
		workspace[zLineOffset+4*(i-16)+3] = right[18*(1+i)]

		a, b := synthWindow(workspace, zLineOffset, i, synthesizeWindowTable[windowIndex:windowIndex+16])
		windowIndex += 16

		if channelCount == 2 {
			samples[(15-i)*2+1] = a[1] / 32768.0
			samples[(17+i)*2+1] = b[1] / 32768.0
			samples[(47-i)*2+1] = a[3] / 32768.0
			samples[(49+i)*2+1] = b[3] / 32768.0
		}
		samples[(15-i)*channelCount] = a[0] / 32768.0
		samples[(17+i)*channelCount] = b[0] / 32768.0
		samples[(47-i)*channelCount] = a[2] / 32768.0
		samples[(49+i)*channelCount] = b[2] / 32768.0
	}
}

func (d *Decoder) synthesizeGranule(granule []float32, bandCount int, channelCount int, pcmSamples []float32) {
	for i := 0; i < channelCount; i++ {
		dctType2(granule[layer3.SamplesPerGranule*i:], bandCount)
	}

	d.synthesis.Grow(bandCount * 64)
	window := d.synthesis.Data()
	switch {
	case channelCount == 1 && d.synthesisChannels != 1:
		for i := 1; i < synthHistoryLength; i += 2 {
			d.synthesisOdd[i/2] = window[i]
		}
	case channelCount == 2 && d.synthesisChannels == 1:
		for i := 1; i < synthHistoryLength; i += 2 {
			window[i] = d.synthesisOdd[i/2]
		}
	}

	for i := 0; i < bandCount; i += 2 {
		synthesizeFloat(granule[i:], pcmSamples[32*channelCount*i:], channelCount, window[i*64:])
	}

	d.synthesis.Discard(bandCount * 64)
	d.synthesisChannels = channelCount
}
