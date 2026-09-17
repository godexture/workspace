package decode

var dctCosine = [24]float32{
	10.19000816, 0.50060302, 0.50241929, 3.40760851, 0.50547093, 0.52249861, 2.05778098, 0.51544732, 0.56694406, 1.48416460, 0.53104258, 0.64682180, 1.16943991, 0.55310392, 0.78815460, 0.97256821, 0.58293498, 1.06067765, 0.83934963, 0.62250412, 1.72244716, 0.74453628, 0.67480832, 5.10114861,
}

func dct(granule []float32, bandCount int) {
	cosine := dctCosine

	for k := 0; k < bandCount; k++ {
		var temporary [4][8]float32
		yIndex := k

		for i := 0; i < 8; i++ {
			x0 := granule[yIndex+i*granuleStride]
			x1 := granule[yIndex+(15-i)*granuleStride]
			x2 := granule[yIndex+(16+i)*granuleStride]
			x3 := granule[yIndex+(31-i)*granuleStride]
			t0 := x0 + x3
			t1 := x1 + x2
			t2 := (x1 - x2) * cosine[3*i]
			t3 := (x0 - x3) * cosine[3*i+1]
			temporary[0][i] = t0 + t1
			temporary[1][i] = (t0 - t1) * cosine[3*i+2]
			temporary[2][i] = t3 + t2
			temporary[3][i] = (t3 - t2) * cosine[3*i+2]
		}
		for i := 0; i < 4; i++ {
			x0 := temporary[i][0]
			x1 := temporary[i][1]
			x2 := temporary[i][2]
			x3 := temporary[i][3]
			x4 := temporary[i][4]
			x5 := temporary[i][5]
			x6 := temporary[i][6]
			x7 := temporary[i][7]

			xTemporary := x0 - x7
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
			temporary[i][0] = x0 + x1
			temporary[i][4] = (x0 - x1) * 0.70710677
			x5 = x5 + x6
			x6 = (x6 + x7) * 0.70710677
			x7 = x7 + xTemporary
			x3 = (x3 + x4) * 0.70710677
			x5 -= x7 * 0.198912367
			x7 += x5 * 0.382683432
			x5 -= x7 * 0.198912367
			x0 = xTemporary - x6
			xTemporary += x6
			temporary[i][1] = (xTemporary + x7) * 0.50979561
			temporary[i][2] = (x4 + x3) * 0.54119611
			temporary[i][3] = (x0 - x5) * 0.60134488
			temporary[i][5] = (x0 + x5) * 0.89997619
			temporary[i][6] = (x4 - x3) * 1.30656302
			temporary[i][7] = (xTemporary - x7) * 2.56291556
		}
		for i := 0; i < 7; i++ {
			granule[yIndex] = temporary[0][i]
			granule[yIndex+granuleStride] = temporary[2][i] + temporary[3][i] + temporary[3][i+1]
			granule[yIndex+2*granuleStride] = temporary[1][i] + temporary[1][i+1]
			granule[yIndex+3*granuleStride] = temporary[2][i+1] + temporary[3][i] + temporary[3][i+1]
			yIndex += 4 * granuleStride
		}
		granule[yIndex] = temporary[0][7]
		granule[yIndex+granuleStride] = temporary[2][7] + temporary[3][7]
		granule[yIndex+2*granuleStride] = temporary[1][7]
		granule[yIndex+3*granuleStride] = temporary[3][7]
	}
}
