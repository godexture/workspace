package mp3

// FloatToS16 converts float32 PCM samples to int16 PCM samples.
func FloatToS16(in []float32, out []int16) {
	if len(in) == 0 || len(out) == 0 {
		return
	}
	n := len(in)
	if len(out) < n {
		n = len(out)
	}
	for i := 0; i < n; i++ {
		sample := in[i] * 32768.0
		if sample >= 32766.5 {
			out[i] = 32767
		} else if sample <= -32767.5 {
			out[i] = -32768
		} else {
			var s int16
			if sample >= 0 {
				s = int16(sample + 0.5)
			} else {
				s = int16(sample - 0.5)
			}
			out[i] = s
		}
	}
}

func mp3dSynthPairFloat(pcm []float32, pcmOffset int, nch int, z []float32, zOffset int) {
	a := (z[zOffset+14*64] - z[zOffset]) * 29
	a += (z[zOffset+1*64] + z[zOffset+13*64]) * 213
	a += (z[zOffset+12*64] - z[zOffset+2*64]) * 459
	a += (z[zOffset+3*64] + z[zOffset+11*64]) * 2037
	a += (z[zOffset+10*64] - z[zOffset+4*64]) * 5153
	a += (z[zOffset+5*64] + z[zOffset+9*64]) * 6574
	a += (z[zOffset+8*64] - z[zOffset+6*64]) * 37489
	a += z[zOffset+7*64] * 75038
	pcm[pcmOffset] = a / 32768.0

	zOffset += 2
	a = z[zOffset+14*64] * 104
	a += z[zOffset+12*64] * 1567
	a += z[zOffset+10*64] * 9727
	a += z[zOffset+8*64] * 64019
	a += z[zOffset+6*64] * -9975
	a += z[zOffset+4*64] * -45
	a += z[zOffset+2*64] * 146
	a += z[zOffset] * -5
	pcm[pcmOffset+16*nch] = a / 32768.0
}

func mp3dSynthFloat(xl []float32, xlOffset int, dstl []float32, dstlOffset int, nch int, lins []float32, linsOffset int) {
	xrOffset := xlOffset + 576*(nch-1)
	dstrOffset := dstlOffset + (nch - 1)

	gWin := [15 * 16]float32{
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

	zlinOffset := linsOffset + 15*64
	wIdx := 0

	lins[zlinOffset+4*15] = xl[xlOffset+18*16]
	lins[zlinOffset+4*15+1] = xl[xrOffset+18*16]
	lins[zlinOffset+4*15+2] = xl[xlOffset+0]
	lins[zlinOffset+4*15+3] = xl[xrOffset+0]

	lins[zlinOffset+4*31] = xl[xlOffset+1+18*16]
	lins[zlinOffset+4*31+1] = xl[xrOffset+1+18*16]
	lins[zlinOffset+4*31+2] = xl[xlOffset+1]
	lins[zlinOffset+4*31+3] = xl[xrOffset+1]

	mp3dSynthPairFloat(dstl, dstrOffset, nch, lins, linsOffset+4*15+1)
	mp3dSynthPairFloat(dstl, dstrOffset+32*nch, nch, lins, linsOffset+4*15+64+1)
	mp3dSynthPairFloat(dstl, dstlOffset, nch, lins, linsOffset+4*15)
	mp3dSynthPairFloat(dstl, dstlOffset+32*nch, nch, lins, linsOffset+4*15+64)

	for i := 14; i >= 0; i-- {
		lins[zlinOffset+4*i] = xl[xlOffset+18*(31-i)]
		lins[zlinOffset+4*i+1] = xl[xrOffset+18*(31-i)]
		lins[zlinOffset+4*i+2] = xl[xlOffset+1+18*(31-i)]
		lins[zlinOffset+4*i+3] = xl[xrOffset+1+18*(31-i)]

		lins[zlinOffset+4*(i+16)] = xl[xlOffset+1+18*(1+i)]
		lins[zlinOffset+4*(i+16)+1] = xl[xrOffset+1+18*(1+i)]
		lins[zlinOffset+4*(i-16)+2] = xl[xlOffset+18*(1+i)]
		lins[zlinOffset+4*(i-16)+3] = xl[xrOffset+18*(1+i)]

		load := func(k int) (float32, float32, int, int) {
			w0 := gWin[wIdx]
			wIdx++
			w1 := gWin[wIdx]
			wIdx++
			vzIdx := zlinOffset + 4*i - k*64
			vyIdx := zlinOffset + 4*i - (15-k)*64
			return w0, w1, vzIdx, vyIdx
		}

		var a, b [4]float32

		// S0(0)
		{
			w0, w1, vzIdx, vyIdx := load(0)
			for j := 0; j < 4; j++ {
				b[j] = lins[vzIdx+j]*w1 + lins[vyIdx+j]*w0
				a[j] = lins[vzIdx+j]*w0 - lins[vyIdx+j]*w1
			}
		}
		// S2(1)
		{
			w0, w1, vzIdx, vyIdx := load(1)
			for j := 0; j < 4; j++ {
				b[j] += lins[vzIdx+j]*w1 + lins[vyIdx+j]*w0
				a[j] += lins[vyIdx+j]*w1 - lins[vzIdx+j]*w0
			}
		}
		// S1(2)
		{
			w0, w1, vzIdx, vyIdx := load(2)
			for j := 0; j < 4; j++ {
				b[j] += lins[vzIdx+j]*w1 + lins[vyIdx+j]*w0
				a[j] += lins[vzIdx+j]*w0 - lins[vyIdx+j]*w1
			}
		}
		// S2(3)
		{
			w0, w1, vzIdx, vyIdx := load(3)
			for j := 0; j < 4; j++ {
				b[j] += lins[vzIdx+j]*w1 + lins[vyIdx+j]*w0
				a[j] += lins[vyIdx+j]*w1 - lins[vzIdx+j]*w0
			}
		}
		// S1(4)
		{
			w0, w1, vzIdx, vyIdx := load(4)
			for j := 0; j < 4; j++ {
				b[j] += lins[vzIdx+j]*w1 + lins[vyIdx+j]*w0
				a[j] += lins[vzIdx+j]*w0 - lins[vyIdx+j]*w1
			}
		}
		// S2(5)
		{
			w0, w1, vzIdx, vyIdx := load(5)
			for j := 0; j < 4; j++ {
				b[j] += lins[vzIdx+j]*w1 + lins[vyIdx+j]*w0
				a[j] += lins[vyIdx+j]*w1 - lins[vzIdx+j]*w0
			}
		}
		// S1(6)
		{
			w0, w1, vzIdx, vyIdx := load(6)
			for j := 0; j < 4; j++ {
				b[j] += lins[vzIdx+j]*w1 + lins[vyIdx+j]*w0
				a[j] += lins[vzIdx+j]*w0 - lins[vyIdx+j]*w1
			}
		}
		// S2(7)
		{
			w0, w1, vzIdx, vyIdx := load(7)
			for j := 0; j < 4; j++ {
				b[j] += lins[vzIdx+j]*w1 + lins[vyIdx+j]*w0
				a[j] += lins[vyIdx+j]*w1 - lins[vzIdx+j]*w0
			}
		}

		dstl[dstrOffset+(15-i)*nch] = a[1] / 32768.0
		dstl[dstrOffset+(17+i)*nch] = b[1] / 32768.0
		dstl[dstlOffset+(15-i)*nch] = a[0] / 32768.0
		dstl[dstlOffset+(17+i)*nch] = b[0] / 32768.0
		dstl[dstrOffset+(47-i)*nch] = a[3] / 32768.0
		dstl[dstrOffset+(49+i)*nch] = b[3] / 32768.0
		dstl[dstlOffset+(47-i)*nch] = a[2] / 32768.0
		dstl[dstlOffset+(49+i)*nch] = b[2] / 32768.0
	}
}

func mp3dDctII(grbuf []float32, grbufOffset int, n int) {
	gSec := [24]float32{
		10.19000816, 0.50060302, 0.50241929, 3.40760851, 0.50547093, 0.52249861, 2.05778098, 0.51544732, 0.56694406, 1.48416460, 0.53104258, 0.64682180, 1.16943991, 0.55310392, 0.78815460, 0.97256821, 0.58293498, 1.06067765, 0.83934963, 0.62250412, 1.72244716, 0.74453628, 0.67480832, 5.10114861,
	}

	for k := 0; k < n; k++ {
		var t [4][8]float32
		yIdx := grbufOffset + k

		for i := 0; i < 8; i++ {
			x0 := grbuf[yIdx+i*18]
			x1 := grbuf[yIdx+(15-i)*18]
			x2 := grbuf[yIdx+(16+i)*18]
			x3 := grbuf[yIdx+(31-i)*18]
			t0 := x0 + x3
			t1 := x1 + x2
			t2 := (x1 - x2) * gSec[3*i+0]
			t3 := (x0 - x3) * gSec[3*i+1]
			t[0][i] = t0 + t1
			t[1][i] = (t0 - t1) * gSec[3*i+2]
			t[2][i] = t3 + t2
			t[3][i] = (t3 - t2) * gSec[3*i+2]
		}
		for i := 0; i < 4; i++ {
			x0 := t[i][0]
			x1 := t[i][1]
			x2 := t[i][2]
			x3 := t[i][3]
			x4 := t[i][4]
			x5 := t[i][5]
			x6 := t[i][6]
			x7 := t[i][7]

			xt := x0 - x7
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
			t[i][0] = x0 + x1
			t[i][4] = (x0 - x1) * 0.70710677
			x5 = x5 + x6
			x6 = (x6 + x7) * 0.70710677
			x7 = x7 + xt
			x3 = (x3 + x4) * 0.70710677
			x5 -= x7 * 0.198912367
			x7 += x5 * 0.382683432
			x5 -= x7 * 0.198912367
			x0 = xt - x6
			xt += x6
			t[i][1] = (xt + x7) * 0.50979561
			t[i][2] = (x4 + x3) * 0.54119611
			t[i][3] = (x0 - x5) * 0.60134488
			t[i][5] = (x0 + x5) * 0.89997619
			t[i][6] = (x4 - x3) * 1.30656302
			t[i][7] = (xt - x7) * 2.56291556
		}
		for i := 0; i < 7; i++ {
			grbuf[yIdx+0*18] = t[0][i]
			grbuf[yIdx+1*18] = t[2][i] + t[3][i] + t[3][i+1]
			grbuf[yIdx+2*18] = t[1][i] + t[1][i+1]
			grbuf[yIdx+3*18] = t[2][i+1] + t[3][i] + t[3][i+1]
			yIdx += 4 * 18
		}
		grbuf[yIdx+0*18] = t[0][7]
		grbuf[yIdx+1*18] = t[2][7] + t[3][7]
		grbuf[yIdx+2*18] = t[1][7]
		grbuf[yIdx+3*18] = t[3][7]
	}
}

// Mp3dSynthGranuleFloat is the Go native implementation of subband synthesis filtering.
func Mp3dSynthGranuleFloat(qmfState []float32, grbuf []float32, nbands int, nch int, pcm []float32, pcmOffset int, lins []float32) {
	for i := 0; i < nch; i++ {
		mp3dDctII(grbuf, 576*i, nbands)
	}

	copy(lins[:15*64], qmfState[:15*64])

	for i := 0; i < nbands; i += 2 {
		mp3dSynthFloat(grbuf, i, pcm, pcmOffset+32*nch*i, nch, lins, i*64)
	}

	if nch == 1 {
		for i := 0; i < 15*64; i += 2 {
			qmfState[i] = lins[nbands*64+i]
		}
	} else {
		copy(qmfState[:15*64], lins[nbands*64:nbands*64+15*64])
	}
}
