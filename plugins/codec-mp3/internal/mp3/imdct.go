package mp3

var mdctWindow = [2][18]float32{
	{0.99904822, 0.99144486, 0.97629601, 0.95371695, 0.92387953, 0.88701083, 0.84339145, 0.79335334, 0.73727734, 0.04361938, 0.13052619, 0.21643961, 0.30070580, 0.38268343, 0.46174861, 0.53729961, 0.60876143, 0.67559021},
	{1, 1, 1, 1, 1, 1, 0.99144486, 0.92387953, 0.79335334, 0, 0, 0, 0, 0, 0, 0.13052619, 0.38268343, 0.60876143},
}

var twid9 = [18]float32{
	0.73727734, 0.79335334, 0.84339145, 0.88701083, 0.92387953, 0.95371695, 0.97629601, 0.99144486, 0.99904822, 0.67559021, 0.60876143, 0.53729961, 0.46174861, 0.38268343, 0.30070580, 0.21643961, 0.13052619, 0.04361938,
}

func l3Dct39(y []float32) {
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

func imdct36L3(grbuf []float32, overlap []float32, window []float32, nbands int) {
	for j := 0; j < nbands; j++ {
		currGr := grbuf[j*18 : j*18+18]
		currOv := overlap[j*9 : j*9+9]

		var co, si [9]float32
		co[0] = -currGr[0]
		si[0] = currGr[17]
		for i := 0; i < 4; i++ {
			si[8-2*i] = currGr[4*i+1] - currGr[4*i+2]
			co[1+2*i] = currGr[4*i+1] + currGr[4*i+2]
			si[7-2*i] = currGr[4*i+4] - currGr[4*i+3]
			co[2+2*i] = -(currGr[4*i+3] + currGr[4*i+4])
		}
		l3Dct39(co[:])
		l3Dct39(si[:])

		si[1] = -si[1]
		si[3] = -si[3]
		si[5] = -si[5]
		si[7] = -si[7]

		for i := 0; i < 9; i++ {
			ovl := currOv[i]
			sum := co[i]*twid9[9+i] + si[i]*twid9[0+i]
			currOv[i] = co[i]*twid9[0+i] - si[i]*twid9[9+i]
			currGr[i] = ovl*window[0+i] - sum*window[9+i]
			currGr[17-i] = ovl*window[9+i] + sum*window[0+i]
		}
	}
}

func l3Idct3(x0, x1, x2 float32, dst []float32) {
	m1 := x1 * 0.86602540
	a1 := x0 - x2*0.5
	dst[1] = x0 + x2
	dst[0] = a1 + m1
	dst[2] = a1 - m1
}

func imdct12L3(x []float32, xOffset int, dst []float32, overlap []float32) {
	var twid3 = [6]float32{0.79335334, 0.92387953, 0.99144486, 0.60876143, 0.38268343, 0.13052619}
	var co, si [3]float32

	l3Idct3(-x[xOffset+0], x[xOffset+6]+x[xOffset+3], x[xOffset+12]+x[xOffset+9], co[:])
	l3Idct3(x[xOffset+15], x[xOffset+12]-x[xOffset+9], x[xOffset+6]-x[xOffset+3], si[:])
	si[1] = -si[1]

	for i := 0; i < 3; i++ {
		ovl := overlap[i]
		sum := co[i]*twid3[3+i] + si[i]*twid3[0+i]
		overlap[i] = co[i]*twid3[0+i] - si[i]*twid3[3+i]
		dst[i] = ovl*twid3[2-i] - sum*twid3[5-i]
		dst[5-i] = ovl*twid3[5-i] + sum*twid3[2-i]
	}
}

func imdctShortL3(grbuf []float32, overlap []float32, nbands int) {
	for j := 0; j < nbands; j++ {
		currGr := grbuf[j*18:]
		currOv := overlap[j*9:]
		var tmp [18]float32
		copy(tmp[:], currGr[:18])
		copy(currGr[:6], currOv[:6])
		imdct12L3(tmp[:], 0, currGr[6:], currOv[6:])
		imdct12L3(tmp[:], 1, currGr[12:], currOv[6:])
		imdct12L3(tmp[:], 2, currOv, currOv[6:])
	}
}

// L3ImdctGo is the pure Go implementation of IMDCT.
func L3ImdctGo(grbuf []float32, overlap []float32, blockType int, nLongBands int) {
	grbufOffset := 0
	overlapOffset := 0
	if nLongBands > 0 {
		imdct36L3(grbuf, overlap, mdctWindow[0][:], nLongBands)
		grbufOffset += 18 * nLongBands
		overlapOffset += 9 * nLongBands
	}
	if blockType == 2 { // SHORT_BLOCK_TYPE = 2
		imdctShortL3(grbuf[grbufOffset:], overlap[overlapOffset:], 32-nLongBands)
	} else {
		isStop := 0
		if blockType == 3 { // STOP_BLOCK_TYPE = 3
			isStop = 1
		}
		imdct36L3(grbuf[grbufOffset:], overlap[overlapOffset:], mdctWindow[isStop][:], 32-nLongBands)
	}
}

// L3Imdct performs IMDCT on a granule block.
func L3Imdct(grbuf []float32, overlap []float32, blockType int, nLongBands int) {
	L3ImdctGo(grbuf, overlap, blockType, nLongBands)
}
