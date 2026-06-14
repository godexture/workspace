package mp3

// Mp3Dec is the Go decoder state.
type Mp3Dec struct {
	MdctOverlap     [2][9 * 32]float32
	QmfState        [15 * 2 * 32]float32
	Reserv          int
	FreeFormatBytes int
	Header          [4]byte
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
	success := 1

	if mp3Bytes > 4 && dec.Header[0] == 0xff && hdrCompare(dec.Header[:], mp3) {
		frameSize = hdrFrameBytes(mp3, dec.FreeFormatBytes) + hdrPadding(mp3)
		if frameSize != mp3Bytes && (frameSize+4 > mp3Bytes || !hdrCompare(mp3, mp3[frameSize:])) {
			frameSize = 0
		}
	}

	if frameSize == 0 {
		dec.Init()
		freeFormatBytesPtr := &dec.FreeFormatBytes
		ptrFrameBytes := &frameSize
		i = mp3dFindFrame(mp3, mp3Bytes, freeFormatBytesPtr, ptrFrameBytes)
		if frameSize == 0 || i+frameSize > mp3Bytes {
			return 0, Mp3DecFrameInfo{FrameBytes: i}
		}
	}

	hdr := mp3[i : i+4]
	copy(dec.Header[:], hdr)

	info := Mp3DecFrameInfo{
		FrameBytes:  i + frameSize,
		FrameOffset: i,
		Channels:    2,
		Hz:          hdrSampleRateHz(hdr),
		Layer:       4 - hdrGetLayer(hdr),
		BitrateKbps: hdrBitrateKbps(hdr),
	}
	if hdrIsMono(hdr) {
		info.Channels = 1
	}

	if pcm == nil {
		return hdrFrameSamples(hdr), info
	}

	var bsFrame bitStream
	bsFrame.buf = mp3[i+4:]
	bsFrame.pos = 0
	bsFrame.limit = int32((frameSize - 4) * 8)

	if hdrIsCrc(hdr) {
		getBits(&bsFrame, 16)
	}

	var scratch decScratch

	if info.Layer == 3 {
		mainDataBegin := l3ReadSideInfo(&bsFrame, scratch.gr_info[:], hdr)
		if mainDataBegin < 0 || bsFrame.pos > bsFrame.limit {
			dec.Init()
			return 0, info
		}
		if l3RestoreReservoir(dec, &bsFrame, &scratch, mainDataBegin) {
			pcmOffset := 0
			igrLimit := 1
			if hdrTestMpeg1(hdr) {
				igrLimit = 2
			}
			for igr := 0; igr < igrLimit; igr++ {
				scratch.grbuf = [1200]float32{}
				l3Decode(dec, &scratch, scratch.gr_info[:], igr*info.Channels, info.Channels)
				grbufFlat := scratch.grbuf[:]
				synFlat := scratch.syn[:]
				synthGranule(dec.QmfState[:], grbufFlat, 18, info.Channels, pcm, pcmOffset, synFlat)
				pcmOffset += 576 * info.Channels
			}
		} else {
			success = 0
		}
		l3SaveReservoir(dec, &scratch)
	} else {
		var sci l12ScaleInfo
		l12ReadScaleInfo(hdr, &bsFrame, &sci)

		scratch.grbuf = [1200]float32{}

		iVal := 0
		pcmOffset := 0
		grbufFlat := scratch.grbuf[:]
		for igr := 0; igr < 3; igr++ {
			deqVal := l12DequantizeGranule(grbufFlat[iVal:], &bsFrame, &sci, info.Layer|1)
			iVal += deqVal
			if iVal == 12 {
				iVal = 0
				l12ApplyScf384(&sci, sci.Scf[igr:], grbufFlat)
				synFlat := scratch.syn[:]
				synthGranule(dec.QmfState[:], grbufFlat, 12, info.Channels, pcm, pcmOffset, synFlat)
				scratch.grbuf = [1200]float32{}
				pcmOffset += 384 * info.Channels
			}
			if bsFrame.pos > bsFrame.limit {
				dec.Init()
				return 0, info
			}
		}
	}

	return success * hdrFrameSamples(dec.Header[:]), info
}



