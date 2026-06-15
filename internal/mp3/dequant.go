package mp3

type l12ScaleInfo struct {
	Scf         [3 * 64]float32
	TotalBands  uint8
	StereoBands uint8
	Bitalloc    [64]uint8
	Scfcod      [64]uint8
}

type L12SubbandAlloc struct {
	TabOffset    uint8
	CodeTabWidth uint8
	BandCount    uint8
}

var allocL1 = []L12SubbandAlloc{{76, 4, 32}}
var allocL2M2 = []L12SubbandAlloc{{60, 4, 4}, {44, 3, 7}, {44, 2, 19}}
var allocL2M1 = []L12SubbandAlloc{{0, 4, 3}, {16, 4, 8}, {32, 3, 12}, {40, 2, 7}}
var allocL2M1Lowrate = []L12SubbandAlloc{{44, 4, 2}, {44, 3, 10}}

func subbandAllocTableL12(header Header, sci *l12ScaleInfo) []L12SubbandAlloc {
	var alloc []L12SubbandAlloc
	mode := header.StereoMode()
	stereoBands := 32
	if mode == 3 { // MODE_MONO
		stereoBands = 0
	} else if mode == 1 { // MODE_JOINT_STEREO
		stereoBands = (header.StereoModeExt() << 2) + 4
	}

	nbands := 0
	if header.IsLayer1() {
		alloc = allocL1
		nbands = 32
	} else if !header.TestMpeg1() {
		alloc = allocL2M2
		nbands = 30
	} else {
		sampleRateIdx := header.SampleRate()
		kbps := header.BitrateKbps()
		if mode != 3 { // mode != MODE_MONO
			kbps >>= 1
		}
		if kbps == 0 {
			kbps = 192
		}

		alloc = allocL2M1
		nbands = 27
		if kbps < 56 {
			alloc = allocL2M1Lowrate
			if sampleRateIdx == 2 {
				nbands = 12
			} else {
				nbands = 8
			}
		} else if kbps >= 96 && sampleRateIdx != 1 {
			nbands = 30
		}
	}

	sci.TotalBands = uint8(nbands)
	sci.StereoBands = uint8(min(stereoBands, nbands))
	return alloc
}

var deqL12 = [18 * 3]float32{
	9.53674316e-07 / 3.0, 7.56931807e-07 / 3.0, 6.00777173e-07 / 3.0,
	9.53674316e-07 / 7.0, 7.56931807e-07 / 7.0, 6.00777173e-07 / 7.0,
	9.53674316e-07 / 15.0, 7.56931807e-07 / 15.0, 6.00777173e-07 / 15.0,
	9.53674316e-07 / 31.0, 7.56931807e-07 / 31.0, 6.00777173e-07 / 31.0,
	9.53674316e-07 / 63.0, 7.56931807e-07 / 63.0, 6.00777173e-07 / 63.0,
	9.53674316e-07 / 127.0, 7.56931807e-07 / 127.0, 6.00777173e-07 / 127.0,
	9.53674316e-07 / 255.0, 7.56931807e-07 / 255.0, 6.00777173e-07 / 255.0,
	9.53674316e-07 / 511.0, 7.56931807e-07 / 511.0, 6.00777173e-07 / 511.0,
	9.53674316e-07 / 1023.0, 7.56931807e-07 / 1023.0, 6.00777173e-07 / 1023.0,
	9.53674316e-07 / 2047.0, 7.56931807e-07 / 2047.0, 6.00777173e-07 / 2047.0,
	9.53674316e-07 / 4095.0, 7.56931807e-07 / 4095.0, 6.00777173e-07 / 4095.0,
	9.53674316e-07 / 8191.0, 7.56931807e-07 / 8191.0, 6.00777173e-07 / 8191.0,
	9.53674316e-07 / 16383.0, 7.56931807e-07 / 16383.0, 6.00777173e-07 / 16383.0,
	9.53674316e-07 / 32767.0, 7.56931807e-07 / 32767.0, 6.00777173e-07 / 32767.0,
	9.53674316e-07 / 65535.0, 7.56931807e-07 / 65535.0, 6.00777173e-07 / 65535.0,
	9.53674316e-07 / 3.0, 7.56931807e-07 / 3.0, 6.00777173e-07 / 3.0,
	9.53674316e-07 / 5.0, 7.56931807e-07 / 5.0, 6.00777173e-07 / 5.0,
	9.53674316e-07 / 9.0, 7.56931807e-07 / 9.0, 6.00777173e-07 / 9.0,
}

func l12ReadScalefactors(bs *bitReader, pba []uint8, scfcod []uint8, bands int, scf []float32) {
	pbaIdx := 0
	scfIdx := 0
	for i := 0; i < bands; i++ {
		var s float32 = 0
		ba := int(pba[pbaIdx])
		pbaIdx++
		mask := 0
		if ba != 0 {
			mask = 4 + int((19>>scfcod[i])&3)
		}
		for m := 4; m > 0; m >>= 1 {
			if (mask & m) != 0 {
				b := int(bs.getBits(6))
				s = deqL12[ba*3-6+b%3] * float32(int(1<<21)>>(b/3))
			}
			scf[scfIdx] = s
			scfIdx++
		}
	}
}

var bitallocCodeTab = []byte{
	0, 17, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
	0, 17, 18, 3, 19, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 16,
	0, 17, 18, 3, 19, 4, 5, 16,
	0, 17, 18, 16,
	0, 17, 18, 19, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
	0, 17, 18, 3, 19, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14,
	0, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
}

func readScaleInfoL12(header Header, bs *bitReader, sci *l12ScaleInfo) {
	subbandAlloc := subbandAllocTableL12(header, sci)

	k := 0
	baBits := 0
	baCodeTabOffset := 0
	subbandAllocIdx := 0

	for i := 0; i < int(sci.TotalBands); i++ {
		var ba byte
		if i == k {
			sa := subbandAlloc[subbandAllocIdx]
			subbandAllocIdx++
			k += int(sa.BandCount)
			baBits = int(sa.CodeTabWidth)
			baCodeTabOffset = int(sa.TabOffset)
		}
		bits := int(bs.getBits(baBits))
		ba = bitallocCodeTab[baCodeTabOffset+bits]
		sci.Bitalloc[2*i] = ba
		if i < int(sci.StereoBands) {
			bits = int(bs.getBits(baBits))
			ba = bitallocCodeTab[baCodeTabOffset+bits]
		}
		if sci.StereoBands != 0 {
			sci.Bitalloc[2*i+1] = ba
		} else {
			sci.Bitalloc[2*i+1] = 0
		}
	}

	for i := 0; i < 2*int(sci.TotalBands); i++ {
		if sci.Bitalloc[i] != 0 {
			if header.IsLayer1() {
				sci.Scfcod[i] = 2
			} else {
				sci.Scfcod[i] = byte(bs.getBits(2))
			}
		} else {
			sci.Scfcod[i] = 6
		}
	}

	l12ReadScalefactors(bs, sci.Bitalloc[:], sci.Scfcod[:], int(sci.TotalBands*2), sci.Scf[:])

	for i := int(sci.StereoBands); i < int(sci.TotalBands); i++ {
		sci.Bitalloc[2*i+1] = 0
	}
}

func dequantizeGranuleL12(grbuf []float32, bs *bitReader, sci *l12ScaleInfo, groupSize int) int {
	choff := SamplesPerGranuleLayer3
	for j := 0; j < 4; j++ {
		dstOffset := groupSize * j
		for i := 0; i < 2*int(sci.TotalBands); i++ {
			ba := int(sci.Bitalloc[i])
			if ba != 0 {
				if ba < 17 {
					half := (1 << (ba - 1)) - 1
					for k := 0; k < groupSize; k++ {
						grbuf[dstOffset+k] = float32(int(bs.getBits(ba)) - half)
					}
				} else {
					mod := uint32((2 << (ba - 17)) + 1)
					code := bs.getBits(int(mod + 2 - (mod >> 3)))
					for k := 0; k < groupSize; k++ {
						grbuf[dstOffset+k] = float32(int(code%mod) - int(mod/2))
						code /= mod
					}
				}
			}
			dstOffset += choff
			choff = 18 - choff
		}
	}
	return groupSize * 4
}

func applyScf384L12(sci *l12ScaleInfo, scf []float32, dst []float32) {
	copy(dst[SamplesPerGranuleLayer3+int(sci.StereoBands)*SamplesPerSubbandLayer3:SamplesPerGranuleLayer3+int(sci.TotalBands)*SamplesPerSubbandLayer3], dst[int(sci.StereoBands)*SamplesPerSubbandLayer3:int(sci.TotalBands)*SamplesPerSubbandLayer3])
	dstOffset := 0
	scfOffset := 0
	for i := 0; i < int(sci.TotalBands); i++ {
		for k := 0; k < SamplesPerSubbandLayer12; k++ {
			dst[dstOffset+k] *= scf[scfOffset+0]
			dst[dstOffset+k+SamplesPerGranuleLayer3] *= scf[scfOffset+3]
		}
		dstOffset += SamplesPerSubbandLayer3
		scfOffset += 6
	}
}

type decoderWorkspace struct {
	bs       bitReader
	maindata [MaxBitreservoirBytes + MaxFreeFormatFrameSize]byte
	gr_info  [4]grInfo
	grbuf    [MaxGranuleBufferSize]float32
	scf      [MaxScalefactorBands]float32
	syn      [2112]float32
	ist_pos  [MaxChannels][MaxScalefactorBands]byte
}

func changeSignL3(grbuf []float32) {
	for b := 0; b < NumSubbands; b += 2 {
		offset := (b + 1) * SamplesPerSubbandLayer3
		for i := 1; i < SamplesPerSubbandLayer3; i += 2 {
			grbuf[offset+i] = -grbuf[offset+i]
		}
	}
}

func reorderL3(grbuf []float32, scratch []float32, sfb []byte) {
	srcIdx := 0
	dstIdx := 0
	sfbIdx := 0
	for {
		length := int(sfb[sfbIdx])
		if length == 0 {
			break
		}
		sfbIdx += 3
		for i := 0; i < length; i++ {
			scratch[dstIdx] = grbuf[srcIdx+0*length]
			dstIdx++
			scratch[dstIdx] = grbuf[srcIdx+1*length]
			dstIdx++
			scratch[dstIdx] = grbuf[srcIdx+2*length]
			dstIdx++
			srcIdx++
		}
		srcIdx += 2 * length
	}
	copy(grbuf[:dstIdx], scratch[:dstIdx])
}

var aa = [2][8]float32{
	{0.85749293, 0.88174200, 0.94962865, 0.98331459, 0.99551782, 0.99916056, 0.99989920, 0.99999316},
	{0.51449576, 0.47173197, 0.31337745, 0.18191320, 0.09457419, 0.04096558, 0.01419856, 0.00369997},
}

func antialiasL3(grbuf []float32, nbands int) {
	idx := 0
	for ; nbands > 0; nbands-- {
		for i := 0; i < (SamplesPerSubbandLayer3/2)-1; i++ {
			u := grbuf[idx+SamplesPerSubbandLayer3+i]
			d := grbuf[idx+(SamplesPerSubbandLayer3-1)-i]
			grbuf[idx+SamplesPerSubbandLayer3+i] = u*aa[0][i] - d*aa[1][i]
			grbuf[idx+(SamplesPerSubbandLayer3-1)-i] = u*aa[1][i] + d*aa[0][i]
		}
		idx += SamplesPerSubbandLayer3
	}
}

func stereoTopBandL3(right []float32, sfb []byte, nbands int, maxBand []int) {
	maxBand[0] = -1
	maxBand[1] = -1
	maxBand[2] = -1

	idx := 0
	for i := 0; i < nbands; i++ {
		sfbVal := int(sfb[i])
		for k := 0; k < sfbVal; k += 2 {
			if right[idx+k] != 0 || right[idx+k+1] != 0 {
				maxBand[i%3] = i
				break
			}
		}
		idx += sfbVal
	}
}

func intensityStereoBandL3(left []float32, n int, kl float32, kr float32) {
	for i := 0; i < n; i++ {
		left[i+SamplesPerGranuleLayer3] = left[i] * kr
		left[i] = left[i] * kl
	}
}

func midsideStereoL3(left []float32, n int) {
	for i := 0; i < n; i++ {
		a := left[i]
		b := left[i+SamplesPerGranuleLayer3]
		left[i] = a + b
		left[i+SamplesPerGranuleLayer3] = a - b
	}
}

var pan = [14]float32{0, 1, 0.21132487, 0.78867513, 0.36602540, 0.63397460, 0.5, 0.5, 0.63397460, 0.36602540, 0.78867513, 0.21132487, 1, 0}

func stereoProcessL3(left []float32, istPos []byte, sfb []byte, header Header, maxBand []int, mpeg2Sh int) {
	maxPos := 64
	if header.TestMpeg1() {
		maxPos = 7
	}

	idx := 0
	for i := 0; ; i++ {
		sfbVal := int(sfb[i])
		if sfbVal == 0 {
			break
		}
		ipos := int(istPos[i])
		if i > maxBand[i%3] && ipos < maxPos {
			var kl, kr float32
			var s float32 = 1.0
			if header.TestMsStereo() {
				s = 1.41421356
			}
			if header.TestMpeg1() {
				kl = pan[2*ipos]
				kr = pan[2*ipos+1]
			} else {
				kl = 1.0
				kr = L3LdexpQ2(1.0, ((ipos+1)>>1)<<mpeg2Sh)
				if (ipos & 1) != 0 {
					kl = kr
					kr = 1.0
				}
			}
			intensityStereoBandL3(left[idx:], sfbVal, kl*s, kr*s)
		} else if header.TestMsStereo() {
			midsideStereoL3(left[idx:], sfbVal)
		}
		idx += sfbVal
	}
}

func readScalefactorsL3(scf []byte, istPos []byte, scfSize []byte, scfCount []byte, bitbuf *bitReader, scfsi int) {
	scfIdx := 0
	istIdx := 0
	partIdx := 0
	for i := 0; i < 4 && scfCount[partIdx+i] != 0; i++ {
		cnt := int(scfCount[partIdx+i])
		if (scfsi & 8) != 0 {
			copy(scf[scfIdx:scfIdx+cnt], istPos[istIdx:istIdx+cnt])
		} else {
			bits := int(scfSize[i])
			if bits == 0 {
				for k := 0; k < cnt; k++ {
					scf[scfIdx+k] = 0
					istPos[istIdx+k] = 0
				}
			} else {
				maxScf := -1
				if scfsi < 0 {
					maxScf = (1 << bits) - 1
				}
				for k := 0; k < cnt; k++ {
					s := int(bitbuf.getBits(bits))
					if s == maxScf {
						istPos[istIdx+k] = 255
					} else {
						istPos[istIdx+k] = byte(s)
					}
					scf[scfIdx+k] = byte(s)
				}
			}
		}
		istIdx += cnt
		scfIdx += cnt
		scfsi *= 2
	}
	scf[scfIdx+0] = 0
	scf[scfIdx+1] = 0
	scf[scfIdx+2] = 0
}

func intensityStereoL3(left []float32, istPos []byte, gr *grInfo, gr1 *grInfo, header Header) {
	var maxBand [3]int
	nSfb := int(gr.nLongSfb + gr.nShortSfb)
	maxBlocks := 1
	if gr.nShortSfb != 0 {
		maxBlocks = 3
	}

	stereoTopBandL3(left[SamplesPerGranuleLayer3:], gr.sfbtab, nSfb, maxBand[:])
	if gr.nLongSfb != 0 {
		m := max(max(maxBand[0], maxBand[1]), maxBand[2])
		maxBand[0] = m
		maxBand[1] = m
		maxBand[2] = m
	}
	for i := 0; i < maxBlocks; i++ {
		defaultPos := 0
		if header.TestMpeg1() {
			defaultPos = 3
		}
		itop := nSfb - maxBlocks + i
		prev := itop - maxBlocks
		if maxBand[i] >= prev {
			istPos[itop] = byte(defaultPos)
		} else {
			istPos[itop] = istPos[prev]
		}
	}
	stereoProcessL3(left, istPos, gr.sfbtab, header, maxBand[:], int(gr1.scalefacCompress&1))
}

var expfrac = [4]float32{9.31322575e-10, 7.83145814e-10, 6.58544508e-10, 5.53767716e-10}

func L3LdexpQ2(y float32, expQ2 int) float32 {
	for expQ2 > 0 {
		e := min(120, expQ2)
		y *= expfrac[e&3] * float32(int(1<<30)>>(e>>2))
		expQ2 -= e
	}
	return y
}

var scfPartitions = [3][28]byte{
	{6, 5, 5, 5, 6, 5, 5, 5, 6, 5, 7, 3, 11, 10, 0, 0, 7, 7, 7, 0, 6, 6, 6, 3, 8, 8, 5, 0},
	{8, 9, 6, 12, 6, 9, 9, 9, 6, 9, 12, 6, 15, 18, 0, 0, 6, 15, 12, 0, 6, 12, 9, 6, 6, 18, 9, 0},
	{9, 9, 6, 12, 9, 9, 9, 9, 9, 9, 12, 6, 18, 18, 0, 0, 12, 12, 12, 0, 12, 9, 9, 6, 15, 12, 9, 0},
}

var scfcDecode = [16]byte{0, 1, 2, 3, 12, 5, 6, 7, 9, 10, 11, 13, 14, 15, 18, 19}
var scfMod = [24]byte{5, 5, 4, 4, 5, 5, 4, 1, 4, 3, 1, 1, 5, 6, 6, 1, 4, 4, 4, 1, 4, 3, 1, 1}

func decodeScalefactorsL3(header Header, istPos []byte, bs *bitReader, gr *grInfo, scf []float32, ch int) {
	partitionIdx := 0
	if gr.nShortSfb != 0 {
		partitionIdx += 1
	}
	if gr.nLongSfb == 0 {
		partitionIdx += 1
	}
	scfPartition := scfPartitions[partitionIdx][:]

	var scfSize [4]byte
	var iscf [MaxScalefactorBands]byte
	scfShift := int(gr.scalefacScale + 1)
	scfsi := int(gr.scfsi)

	if header.TestMpeg1() {
		part := int(scfcDecode[gr.scalefacCompress])
		scfSize[0] = byte(part >> 2)
		scfSize[1] = byte(part >> 2)
		scfSize[2] = byte(part & 3)
		scfSize[3] = byte(part & 3)
	} else {
		ist := 0
		if header.TestIStereo() && ch != 0 {
			ist = 1
		}
		sfc := int(gr.scalefacCompress >> ist)
		k := ist * 12
		for ; sfc >= 0; k += 4 {
			modprod := 1
			for i := 3; i >= 0; i-- {
				scfSize[i] = byte((sfc / modprod) % int(scfMod[k+i]))
				modprod *= int(scfMod[k+i])
			}
			sfc -= modprod
		}
		scfPartition = scfPartition[k:]
		scfsi = -16
	}

	readScalefactorsL3(iscf[:], istPos, scfSize[:], scfPartition, bs, scfsi)

	if gr.nShortSfb != 0 {
		sh := 3 - scfShift
		for i := 0; i < int(gr.nShortSfb); i += 3 {
			iscf[int(gr.nLongSfb)+i+0] += gr.subblockGain[0] << sh
			iscf[int(gr.nLongSfb)+i+1] += gr.subblockGain[1] << sh
			iscf[int(gr.nLongSfb)+i+2] += gr.subblockGain[2] << sh
		}
	} else if gr.preflag != 0 {
		preamp := [10]byte{1, 1, 1, 1, 2, 2, 3, 3, 3, 2}
		for i := 0; i < 10; i++ {
			iscf[11+i] += preamp[i]
		}
	}

	gainExp := int(gr.globalGain) - 4 - 210
	if header.IsMsStereo() {
		gainExp -= 2
	}
	gain := L3LdexpQ2(2048.0, 44-gainExp)
	for i := 0; i < int(gr.nLongSfb+gr.nShortSfb); i++ {
		scf[i] = L3LdexpQ2(gain, int(iscf[i])<<scfShift)
	}
}

func restoreReservoirL3(h *Mp3Dec, bs *bitReader, s *decoderWorkspace, mainDataBegin int) error {
	frameBytes := int((bs.limit - bs.pos) / 8)
	bytesHave := min(h.Reserv, mainDataBegin)

	startIdx := h.Reserv - mainDataBegin
	if startIdx < 0 {
		startIdx = 0
	}
	copy(s.maindata[:], h.ReservBuf[startIdx:startIdx+bytesHave])

	copy(s.maindata[bytesHave:], bs.buf[int(bs.pos/8):int(bs.pos/8)+frameBytes])

	s.bs.buf = s.maindata[:]
	s.bs.pos = 0
	s.bs.limit = int32((bytesHave + frameBytes) * 8)

	if h.Reserv < mainDataBegin {
		return ErrInsufficientReservoir
	}
	return nil
}

func saveReservoirL3(h *Mp3Dec, s *decoderWorkspace) {
	pos := int((s.bs.pos + 7) / 8)
	remains := int(s.bs.limit/8) - pos
	if remains > MaxBitreservoirBytes {
		pos += remains - MaxBitreservoirBytes
		remains = MaxBitreservoirBytes
	}
	if remains > 0 {
		copy(h.ReservBuf[:remains], s.maindata[pos:pos+remains])
	}
	h.Reserv = remains
}

var scfLong = [8][23]byte{
	{6, 6, 6, 6, 6, 6, 8, 10, 12, 14, 16, 20, 24, 28, 32, 38, 46, 52, 60, 68, 58, 54, 0},
	{12, 12, 12, 12, 12, 12, 16, 20, 24, 28, 32, 40, 48, 56, 64, 76, 90, 2, 2, 2, 2, 2, 0},
	{6, 6, 6, 6, 6, 6, 8, 10, 12, 14, 16, 20, 24, 28, 32, 38, 46, 52, 60, 68, 58, 54, 0},
	{6, 6, 6, 6, 6, 6, 8, 10, 12, 14, 16, 18, 22, 26, 32, 38, 46, 54, 62, 70, 76, 36, 0},
	{6, 6, 6, 6, 6, 6, 8, 10, 12, 14, 16, 20, 24, 28, 32, 38, 46, 52, 60, 68, 58, 54, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 8, 8, 10, 12, 16, 20, 24, 28, 34, 42, 50, 54, 76, 158, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 10, 12, 16, 18, 22, 28, 34, 40, 46, 54, 54, 192, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 8, 10, 12, 16, 20, 24, 30, 38, 46, 56, 68, 84, 102, 26, 0},
}

var scfShort = [8][MaxScalefactorBands + 1]byte{
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 30, 30, 30, 40, 40, 40, 18, 18, 18, 0},
	{8, 8, 8, 8, 8, 8, 8, 8, 8, 12, 12, 12, 16, 16, 16, 20, 20, 20, 24, 24, 24, 28, 28, 28, 36, 36, 36, 2, 2, 2, 2, 2, 2, 2, 2, 2, 26, 26, 26, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 14, 14, 14, 18, 18, 18, 26, 26, 26, 32, 32, 32, 42, 42, 42, 18, 18, 18, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 32, 32, 32, 44, 44, 44, 12, 12, 12, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 30, 30, 30, 40, 40, 40, 18, 18, 18, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 22, 22, 22, 30, 30, 30, 56, 56, 56, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 6, 6, 6, 10, 10, 10, 12, 12, 12, 14, 14, 14, 16, 16, 16, 20, 20, 20, 26, 26, 26, 66, 66, 66, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 12, 12, 12, 16, 16, 16, 20, 20, 20, 26, 26, 26, 34, 34, 34, 42, 42, 42, 12, 12, 12, 0},
}

var scfMixed = [8][MaxScalefactorBands + 1]byte{
	{6, 6, 6, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 30, 30, 30, 40, 40, 40, 18, 18, 18, 0},
	{12, 12, 12, 4, 4, 4, 8, 8, 8, 12, 12, 12, 16, 16, 16, 20, 20, 20, 24, 24, 24, 28, 28, 28, 36, 36, 36, 2, 2, 2, 2, 2, 2, 2, 2, 2, 26, 26, 26, 0},
	{6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 14, 14, 14, 18, 18, 18, 26, 26, 26, 32, 32, 32, 42, 42, 42, 18, 18, 18, 0},
	{6, 6, 6, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 32, 32, 32, 44, 44, 44, 12, 12, 12, 0},
	{6, 6, 6, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 30, 30, 30, 40, 40, 40, 18, 18, 18, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 22, 22, 22, 30, 30, 30, 56, 56, 56, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 4, 4, 4, 6, 6, 6, 6, 6, 6, 10, 10, 10, 12, 12, 12, 14, 14, 14, 16, 16, 16, 20, 20, 20, 26, 26, 26, 66, 66, 66, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 4, 4, 4, 6, 6, 6, 8, 8, 8, 12, 12, 12, 16, 16, 16, 20, 20, 20, 26, 26, 26, 34, 34, 34, 42, 42, 42, 12, 12, 12, 0},
}

func readSideInfoL3(bs *bitReader, gr []grInfo, header Header) int {
	srIdx := header.MySampleRate()
	if srIdx != 0 {
		srIdx -= 1
	}
	grCount := 2
	if header.IsMono() {
		grCount = 1
	}
	mainDataBegin := 0
	var scfsi uint32 = 0

	if header.TestMpeg1() {
		grCount *= 2
		mainDataBegin = int(bs.getBits(9))
		scfsi = bs.getBits(7 + grCount)
	} else {
		mainDataBegin = int(bs.getBits(8+grCount) >> grCount)
	}

	part23Sum := 0
	initialGrCount := grCount
	for grIdx := 0; grIdx < initialGrCount; grIdx++ {
		if header.IsMono() {
			scfsi <<= 4
		}
		gr[grIdx].part23Length = uint16(bs.getBits(12))
		part23Sum += int(gr[grIdx].part23Length)
		gr[grIdx].bigValues = uint16(bs.getBits(9))
		if gr[grIdx].bigValues*2 > SamplesPerGranuleLayer3 {
			return -1
		}
		gr[grIdx].globalGain = uint8(bs.getBits(8))
		compressBits := 9
		if header.TestMpeg1() {
			compressBits = 4
		}
		gr[grIdx].scalefacCompress = uint16(bs.getBits(compressBits))
		gr[grIdx].sfbtab = scfLong[srIdx][:]
		gr[grIdx].nLongSfb = 22
		gr[grIdx].nShortSfb = 0
		if bs.getBits(1) != 0 {
			gr[grIdx].blockType = uint8(bs.getBits(2))
			if gr[grIdx].blockType == 0 {
				return -1
			}
			gr[grIdx].mixedBlockFlag = uint8(bs.getBits(1))
			gr[grIdx].regionCount[0] = 7
			gr[grIdx].regionCount[1] = 255
			if gr[grIdx].blockType == 2 { // SHORT_BLOCK_TYPE = 2
				scfsi &= 0x0F0F
				if gr[grIdx].mixedBlockFlag == 0 {
					gr[grIdx].regionCount[0] = 8
					gr[grIdx].sfbtab = scfShort[srIdx][:]
					gr[grIdx].nLongSfb = 0
					gr[grIdx].nShortSfb = 39
				} else {
					gr[grIdx].sfbtab = scfMixed[srIdx][:]
					if header.TestMpeg1() {
						gr[grIdx].nLongSfb = 8
					} else {
						gr[grIdx].nLongSfb = 6
					}
					gr[grIdx].nShortSfb = 30
				}
			}
			tables := bs.getBits(10)
			tables <<= 5
			gr[grIdx].subblockGain[0] = uint8(bs.getBits(3))
			gr[grIdx].subblockGain[1] = uint8(bs.getBits(3))
			gr[grIdx].subblockGain[2] = uint8(bs.getBits(3))
			gr[grIdx].tableSelect[0] = uint8(tables >> 10)
			gr[grIdx].tableSelect[1] = uint8((tables >> 5) & 31)
			gr[grIdx].tableSelect[2] = uint8(tables & 31)
		} else {
			gr[grIdx].blockType = 0
			gr[grIdx].mixedBlockFlag = 0
			tables := bs.getBits(15)
			gr[grIdx].regionCount[0] = uint8(bs.getBits(4))
			gr[grIdx].regionCount[1] = uint8(bs.getBits(3))
			gr[grIdx].regionCount[2] = 255
			gr[grIdx].tableSelect[0] = uint8(tables >> 10)
			gr[grIdx].tableSelect[1] = uint8((tables >> 5) & 31)
			gr[grIdx].tableSelect[2] = uint8(tables & 31)
		}
		if header.TestMpeg1() {
			gr[grIdx].preflag = uint8(bs.getBits(1))
		} else {
			if gr[grIdx].scalefacCompress >= 500 {
				gr[grIdx].preflag = 1
			} else {
				gr[grIdx].preflag = 0
			}
		}
		gr[grIdx].scalefacScale = uint8(bs.getBits(1))
		gr[grIdx].count1Table = uint8(bs.getBits(1))
		gr[grIdx].scfsi = uint8((scfsi >> 12) & 15)
		scfsi <<= 4
	}

	if part23Sum+int(bs.pos) > int(bs.limit)+mainDataBegin*8 {
		return -1
	}

	return mainDataBegin
}

func decodeL3(h *Mp3Dec, s *decoderWorkspace, grInfo []grInfo, grInfoOffset int, nch int) {
	grbufFlat := s.grbuf[:]
	for ch := 0; ch < nch; ch++ {
		layer3grLimit := int(s.bs.pos) + int(grInfo[grInfoOffset+ch].part23Length)
		decodeScalefactorsL3(h.Header, s.ist_pos[ch][:], &s.bs, &grInfo[grInfoOffset+ch], s.scf[:], ch)
		huffmanDecodeL3(grbufFlat[ch*SamplesPerGranuleLayer3:ch*SamplesPerGranuleLayer3+SamplesPerGranuleLayer3], &s.bs, &grInfo[grInfoOffset+ch], s.scf[:], layer3grLimit)
	}

	if h.Header.TestIStereo() {
		intensityStereoL3(grbufFlat, s.ist_pos[1][:], &grInfo[grInfoOffset], &grInfo[grInfoOffset+1], h.Header)
	} else if h.Header.IsMsStereo() {
		midsideStereoL3(grbufFlat, SamplesPerGranuleLayer3)
	}

	for ch := 0; ch < nch; ch++ {
		gr := &grInfo[grInfoOffset+ch]
		aaBands := 30
		nLongBands := 0
		if gr.mixedBlockFlag != 0 {
			nLongBands = 2
		}
		if h.Header.MySampleRate() == 2 {
			nLongBands <<= 1
		}
		if gr.blockType == 2 { // SHORT_BLOCK_TYPE = 2
			var scratch [SamplesPerGranuleLayer3]float32
			if gr.mixedBlockFlag != 0 {
				aaBands = nLongBands - 1
			} else {
				aaBands = -1
			}
			reorderL3(grbufFlat[ch*SamplesPerGranuleLayer3:ch*SamplesPerGranuleLayer3+SamplesPerGranuleLayer3], scratch[:], gr.sfbtab)
		}
		antialiasL3(grbufFlat[ch*SamplesPerGranuleLayer3:], aaBands+1) // Actually aaBands is now fixed to 30, so +1 is 31
		L3Imdct(grbufFlat[ch*SamplesPerGranuleLayer3:ch*SamplesPerGranuleLayer3+SamplesPerGranuleLayer3], h.MdctOverlap[ch][:], int(gr.blockType), nLongBands)
		changeSignL3(grbufFlat[ch*SamplesPerGranuleLayer3 : ch*SamplesPerGranuleLayer3+SamplesPerGranuleLayer3])
	}
}
