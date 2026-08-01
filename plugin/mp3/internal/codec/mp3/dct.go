package mp3

var dctType2CosineCoefficients = [24]float32{
	10.19000816, 0.50060302, 0.50241929, 3.40760851, 0.50547093, 0.52249861, 2.05778098, 0.51544732, 0.56694406, 1.48416460, 0.53104258, 0.64682180, 1.16943991, 0.55310392, 0.78815460, 0.97256821, 0.58293498, 1.06067765, 0.83934963, 0.62250412, 1.72244716, 0.74453628, 0.67480832, 5.10114861,
}

// dctType2Scalar applies the 32-point DCT-II (as used by the polyphase
// synthesis filter) independently for each of the bandCount columns of
// granule. Column k lives at granule[k], granule[k+18], granule[k+2*18], ...
// so distinct k are fully independent: they only ever read/write their own
// column.
func dctType2Scalar(granule []float32, bandCount int) {
	cosineCoefficients := dctType2CosineCoefficients

	for k := 0; k < bandCount; k++ {
		var temp [4][8]float32
		yIndex := k

		for i := 0; i < 8; i++ {
			x0 := granule[yIndex+i*18]
			x1 := granule[yIndex+(15-i)*18]
			x2 := granule[yIndex+(16+i)*18]
			x3 := granule[yIndex+(31-i)*18]
			t0 := x0 + x3
			t1 := x1 + x2
			t2 := (x1 - x2) * cosineCoefficients[3*i+0]
			t3 := (x0 - x3) * cosineCoefficients[3*i+1]
			temp[0][i] = t0 + t1
			temp[1][i] = (t0 - t1) * cosineCoefficients[3*i+2]
			temp[2][i] = t3 + t2
			temp[3][i] = (t3 - t2) * cosineCoefficients[3*i+2]
		}
		for i := 0; i < 4; i++ {
			x0 := temp[i][0]
			x1 := temp[i][1]
			x2 := temp[i][2]
			x3 := temp[i][3]
			x4 := temp[i][4]
			x5 := temp[i][5]
			x6 := temp[i][6]
			x7 := temp[i][7]

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
			temp[i][0] = x0 + x1
			temp[i][4] = (x0 - x1) * 0.70710677
			x5 = x5 + x6
			x6 = (x6 + x7) * 0.70710677
			x7 = x7 + xtTemporary
			x3 = (x3 + x4) * 0.70710677
			x5 -= x7 * 0.198912367
			x7 += x5 * 0.382683432
			x5 -= x7 * 0.198912367
			x0 = xtTemporary - x6
			xtTemporary += x6
			temp[i][1] = (xtTemporary + x7) * 0.50979561
			temp[i][2] = (x4 + x3) * 0.54119611
			temp[i][3] = (x0 - x5) * 0.60134488
			temp[i][5] = (x0 + x5) * 0.89997619
			temp[i][6] = (x4 - x3) * 1.30656302
			temp[i][7] = (xtTemporary - x7) * 2.56291556
		}
		for i := 0; i < 7; i++ {
			granule[yIndex+0*18] = temp[0][i]
			granule[yIndex+1*18] = temp[2][i] + temp[3][i] + temp[3][i+1]
			granule[yIndex+2*18] = temp[1][i] + temp[1][i+1]
			granule[yIndex+3*18] = temp[2][i+1] + temp[3][i] + temp[3][i+1]
			yIndex += 4 * 18
		}
		granule[yIndex+0*18] = temp[0][7]
		granule[yIndex+1*18] = temp[2][7] + temp[3][7]
		granule[yIndex+2*18] = temp[1][7]
		granule[yIndex+3*18] = temp[3][7]
	}
}
