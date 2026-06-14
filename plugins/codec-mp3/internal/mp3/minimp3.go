package mp3

// Mp3Dec is the Go decoder state.
type Mp3Dec struct {
	MdctOverlap     [2][9 * 32]float32
	QmfState        [15 * 2 * 32]float32
	Reserv          int
	FreeFormatBytes int
	Header          Header
	ReservBuf       [511]byte
}

// Mp3DecFrameInfo contains parsed MPEG frame header information.
type Mp3DecFrameInfo struct {
	FrameBytes  int
	FrameOffset int
	Channels    int
	Hz          int
	Layer       int
	BitrateKbps int
}

// Init initializes the decoder.
func (dec *Mp3Dec) Init() {
	*dec = Mp3Dec{}
}

// DecodeFrame decodes one MP3 frame to float32 samples.
// pcm slice must be pre-allocated to hold up to MINIMP3_MAX_SAMPLES_PER_FRAME (1152*2) samples.
// Returns the number of samples decoded *per channel*, and the frame info.
func (dec *Mp3Dec) DecodeFrame(mp3 []byte, pcm []float32) (int, Mp3DecFrameInfo) {
	mp3Bytes := len(mp3)
	frameSize := 0
	i := 0
	success := true

	if mp3Bytes > 4 && dec.Header[0] == 0xff {
		nextHdr, ok := ParseHeader(mp3)
		if ok && dec.Header.Compare(nextHdr) {
			frameSize = nextHdr.FrameBytes(dec.FreeFormatBytes) + nextHdr.Padding()
			if frameSize != mp3Bytes {
				if frameSize+4 > mp3Bytes {
					frameSize = 0
				} else {
					nextHdr2, ok2 := ParseHeader(mp3[frameSize:])
					if !ok2 || !nextHdr.Compare(nextHdr2) {
						frameSize = 0
					}
				}
			}
		}
	}

	if frameSize == 0 {
		dec.Init()
		var found bool
		i, frameSize, dec.FreeFormatBytes, found = FindFrame(mp3, dec.FreeFormatBytes)
		if !found || i+frameSize > mp3Bytes {
			return 0, Mp3DecFrameInfo{FrameBytes: i}
		}
	}

	hdr, ok := ParseHeader(mp3[i : i+4])
	if !ok {
		return 0, Mp3DecFrameInfo{}
	}
	dec.Header = hdr

	info := Mp3DecFrameInfo{
		FrameBytes:  i + frameSize,
		FrameOffset: i,
		Channels:    2,
		Hz:          hdr.SampleRateHz(),
		Layer:       4 - hdr.Layer(),
		BitrateKbps: hdr.BitrateKbps(),
	}
	if hdr.IsMono() {
		info.Channels = 1
	}

	if pcm == nil {
		return hdr.FrameSamples(), info
	}

	var bsFrame bitReader
	bsFrame.buf = mp3[i+4:]
	bsFrame.pos = 0
	bsFrame.limit = int32((frameSize - 4) * 8)

	if hdr.IsCrc() {
		bsFrame.getBits(16)
	}

	var scratch decScratch

	if info.Layer == 3 {
		success = dec.decodeLayer3(info, &bsFrame, &scratch, pcm, hdr)
	} else {
		success = dec.decodeLayer12(info, &bsFrame, &scratch, pcm, hdr)
	}

	if !success {
		return 0, info
	}
	return dec.Header.FrameSamples(), info
}

func (dec *Mp3Dec) decodeLayer3(info Mp3DecFrameInfo, bsFrame *bitReader, scratch *decScratch, pcm []float32, hdr Header) bool {
	mainDataBegin := readSideInfoL3(bsFrame, scratch.gr_info[:], hdr)
	if mainDataBegin < 0 || bsFrame.pos > bsFrame.limit {
		dec.Init()
		return false
	}
	success := true
	if restoreReservoirL3(dec, bsFrame, scratch, mainDataBegin) {
		pcmOffset := 0
		igrLimit := 1
		if hdr.TestMpeg1() {
			igrLimit = 2
		}
		for igr := 0; igr < igrLimit; igr++ {
			scratch.grbuf = [1200]float32{}
			decodeL3(dec, scratch, scratch.gr_info[:], igr*info.Channels, info.Channels)
			synthGranule(dec.QmfState[:], scratch.grbuf[:], 18, info.Channels, pcm[pcmOffset:], scratch.syn[:])
			pcmOffset += 576 * info.Channels
		}
	} else {
		success = false
	}
	saveReservoirL3(dec, scratch)
	return success
}

func (dec *Mp3Dec) decodeLayer12(info Mp3DecFrameInfo, bsFrame *bitReader, scratch *decScratch, pcm []float32, hdr Header) bool {
	var sci l12ScaleInfo
	readScaleInfoL12(hdr, bsFrame, &sci)

	scratch.grbuf = [1200]float32{}

	iVal := 0
	pcmOffset := 0
	grbufFlat := scratch.grbuf[:]
	for igr := 0; igr < 3; igr++ {
		deqVal := dequantizeGranuleL12(grbufFlat[iVal:], bsFrame, &sci, info.Layer|1)
		iVal += deqVal
		if iVal == 12 {
			iVal = 0
			applyScf384L12(&sci, sci.Scf[igr:], grbufFlat)
			synthGranule(dec.QmfState[:], grbufFlat, 12, info.Channels, pcm[pcmOffset:], scratch.syn[:])
			scratch.grbuf = [1200]float32{}
			pcmOffset += 384 * info.Channels
		}
		if bsFrame.pos > bsFrame.limit {
			dec.Init()
			return false
		}
	}
	return true
}
