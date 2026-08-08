package layer3

var aliasReductionCS = [8]float32{0.85749293, 0.88174200, 0.94962865, 0.98331459, 0.99551782, 0.99916056, 0.99989920, 0.99999316}
var aliasReductionCA = [8]float32{0.51449576, 0.47173197, 0.31337745, 0.18191320, 0.09457419, 0.04096558, 0.01419856, 0.00369997}

func antialiasScalar(granule []float32, bandCount int) {
	bandOffset := 0
	for ; bandCount > 0; bandCount-- {
		for i := 0; i < (SamplesPerSubBand/2)-1; i++ {
			upperValue := granule[bandOffset+SamplesPerSubBand+i]
			lowerValue := granule[bandOffset+(SamplesPerSubBand-1)-i]
			granule[bandOffset+SamplesPerSubBand+i] = upperValue*aliasReductionCS[i] - lowerValue*aliasReductionCA[i]
			granule[bandOffset+(SamplesPerSubBand-1)-i] = upperValue*aliasReductionCA[i] + lowerValue*aliasReductionCS[i]
		}
		bandOffset += SamplesPerSubBand
	}
}
