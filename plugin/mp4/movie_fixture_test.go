package mp4

import "encoding/binary"

type fixtureTrack struct {
	id            uint32
	timeScale     uint32
	handler       string
	entryType     string
	size          uint32
	offset        uint64
	offsetDelta   uint64
	compact       bool
	largeOffset   bool
	composition   *fixtureComposition
	sync          []uint32
	directBefore  [][]byte
	directBetween [][]byte
	directAfter   [][]byte
	stblExtra     [][]byte
	sttsExtra     []fixtureTiming
	stsc          []fixtureChunk
}

type fixtureComposition struct {
	version byte
	offset  int32
}

type fixtureTiming struct {
	count    uint32
	duration uint32
}

type fixtureChunk struct {
	first, samples, description uint32
}

func twoTrackMovie(afterMdat bool, major string, compatible ...string) []byte {
	first := fixtureTrack{
		id:        1,
		timeScale: 48000,
		handler:   "soun",
		entryType: "mp4a",
		size:      2,
		sttsExtra: []fixtureTiming{{count: 1, duration: 1024}},
	}
	second := fixtureTrack{
		id:          2,
		timeScale:   1000,
		handler:     "vide",
		entryType:   "avc1",
		size:        3,
		compact:     true,
		largeOffset: true,
		composition: &fixtureComposition{version: 1, offset: -2},
		sync:        []uint32{1},
		sttsExtra:   []fixtureTiming{{count: 1, duration: 40}},
	}
	return fixtureMovie(afterMdat, major, compatible, []fixtureTrack{first, second}, nil, nil)
}

func fixtureMovie(afterMdat bool, major string, compatible []string, tracks []fixtureTrack, topExtras, moovExtras [][]byte) []byte {
	fileType := fixtureFileType(major, compatible...)
	media := make([]byte, 0)
	for _, track := range tracks {
		for index := uint32(0); index < track.size; index++ {
			media = append(media, byte(track.id+index))
		}
	}
	mdat := fixtureBox("mdat", media)
	moov := fixtureMoov(tracks, moovExtras)
	mediaStart := uint64(len(fileType) + 8)
	for _, value := range topExtras {
		mediaStart += uint64(len(value))
	}
	if !afterMdat {
		mediaStart += uint64(len(moov))
	}
	offset := mediaStart
	for index := range tracks {
		tracks[index].offset = offset + tracks[index].offsetDelta
		offset += uint64(tracks[index].size)
	}
	moov = fixtureMoov(tracks, moovExtras)

	result := append([]byte(nil), fileType...)
	for _, value := range topExtras {
		result = append(result, value...)
	}
	if afterMdat {
		result = append(result, mdat...)
		result = append(result, moov...)
	} else {
		result = append(result, moov...)
		result = append(result, mdat...)
	}
	return result
}

func fixtureMoov(tracks []fixtureTrack, extras [][]byte) []byte {
	children := [][]byte{fixtureMVHD()}
	children = append(children, extras...)
	for _, track := range tracks {
		children = append(children, fixtureTrackBox(track))
	}
	return fixtureContainer("moov", children...)
}

func fixtureMVHD() []byte {
	payload := make([]byte, 100)
	binary.BigEndian.PutUint32(payload[12:16], 1000)
	return fixtureBox("mvhd", payload)
}

func fixtureTrackBox(value fixtureTrack) []byte {
	children := append([][]byte(nil), value.directBefore...)
	children = append(children, fixtureTKHD(value.id))
	children = append(children, value.directBetween...)
	children = append(children, fixtureMDIA(value))
	children = append(children, value.directAfter...)
	return fixtureContainer("trak", children...)
}

func fixtureTKHD(id uint32) []byte {
	payload := make([]byte, 84)
	binary.BigEndian.PutUint32(payload[12:16], id)
	return fixtureBox("tkhd", payload)
}

func fixtureMDIA(value fixtureTrack) []byte {
	mdhd := make([]byte, 24)
	binary.BigEndian.PutUint32(mdhd[12:16], value.timeScale)
	hdlr := make([]byte, 24)
	copy(hdlr[8:12], value.handler)
	return fixtureContainer("mdia", fixtureBox("mdhd", mdhd), fixtureBox("hdlr", hdlr), fixtureMINF(value))
}

func fixtureMINF(value fixtureTrack) []byte {
	mediaHeader := fixtureBox("smhd", make([]byte, 8))
	if value.handler == "vide" {
		mediaHeader = fixtureBox("vmhd", make([]byte, 12))
	}
	return fixtureContainer("minf", mediaHeader, fixtureDINF(), fixtureSTBL(value))
}

func fixtureDINF() []byte {
	url := fixtureBox("url ", fixtureFullBox(0, 1, nil))
	dref := append(fixtureFullBox(0, 0, fixtureU32(1)), url...)
	return fixtureContainer("dinf", fixtureBox("dref", dref))
}

func fixtureSTBL(value fixtureTrack) []byte {
	children := [][]byte{
		fixtureSTSD(value.entryType),
		fixtureSTTS(value.sttsExtra),
		fixtureSTSC(value.stsc),
	}
	if value.compact {
		children = append(children, fixtureSTZ2(value.size))
	} else {
		children = append(children, fixtureSTSZ(value.size))
	}
	if value.largeOffset {
		children = append(children, fixtureCO64(value.offset))
	} else {
		children = append(children, fixtureSTCO(uint32(value.offset)))
	}
	if value.composition != nil {
		children = append(children, fixtureCTTS(*value.composition))
	}
	if value.sync != nil {
		children = append(children, fixtureSTSS(value.sync))
	}
	children = append(children, value.stblExtra...)
	return fixtureContainer("stbl", children...)
}

func fixtureSTSD(typeID string) []byte {
	entry := make([]byte, 16)
	binary.BigEndian.PutUint32(entry[:4], uint32(len(entry)))
	copy(entry[4:8], typeID)
	binary.BigEndian.PutUint16(entry[14:16], 1)
	payload := append(fixtureFullBox(0, 0, fixtureU32(1)), entry...)
	return fixtureBox("stsd", payload)
}

func fixtureSTTS(values []fixtureTiming) []byte {
	payload := fixtureFullBox(0, 0, fixtureU32(uint32(len(values))))
	for _, value := range values {
		payload = append(payload, fixtureU32(value.count)...)
		payload = append(payload, fixtureU32(value.duration)...)
	}
	return fixtureBox("stts", payload)
}

func fixtureSTSC(values []fixtureChunk) []byte {
	if values == nil {
		values = []fixtureChunk{{first: 1, samples: 1, description: 1}}
	}
	payload := fixtureFullBox(0, 0, fixtureU32(uint32(len(values))))
	for _, value := range values {
		payload = append(payload, fixtureU32(value.first)...)
		payload = append(payload, fixtureU32(value.samples)...)
		payload = append(payload, fixtureU32(value.description)...)
	}
	return fixtureBox("stsc", payload)
}

func fixtureSTSZ(size uint32) []byte {
	payload := fixtureFullBox(0, 0, fixtureU32(size))
	payload = append(payload, fixtureU32(1)...)
	return fixtureBox("stsz", payload)
}

func fixtureSTZ2(size uint32) []byte {
	payload := fixtureFullBox(0, 0, []byte{0, 0, 0, 8})
	payload = append(payload, fixtureU32(1)...)
	payload = append(payload, byte(size))
	return fixtureBox("stz2", payload)
}

func fixtureSTCO(offset uint32) []byte {
	payload := fixtureFullBox(0, 0, fixtureU32(1))
	payload = append(payload, fixtureU32(offset)...)
	return fixtureBox("stco", payload)
}

func fixtureCO64(offset uint64) []byte {
	payload := fixtureFullBox(0, 0, fixtureU32(1))
	payload = append(payload, fixtureU64(offset)...)
	return fixtureBox("co64", payload)
}

func fixtureCTTS(value fixtureComposition) []byte {
	payload := fixtureFullBox(value.version, 0, fixtureU32(1))
	payload = append(payload, fixtureU32(1)...)
	payload = append(payload, fixtureU32(uint32(value.offset))...)
	return fixtureBox("ctts", payload)
}

func fixtureSTSS(values []uint32) []byte {
	payload := fixtureFullBox(0, 0, fixtureU32(uint32(len(values))))
	for _, value := range values {
		payload = append(payload, fixtureU32(value)...)
	}
	return fixtureBox("stss", payload)
}

func fixtureFileType(major string, compatible ...string) []byte {
	payload := append([]byte(major), fixtureU32(0)...)
	for _, brand := range compatible {
		payload = append(payload, brand...)
	}
	return fixtureBox("ftyp", payload)
}

func fixtureContainer(typeID string, children ...[]byte) []byte {
	var payload []byte
	for _, child := range children {
		payload = append(payload, child...)
	}
	return fixtureBox(typeID, payload)
}

func fixtureBox(typeID string, payload []byte) []byte {
	return appendBox(nil, 8+len(payload), typeID, payload)
}

func fixtureFullBox(version byte, flags uint32, payload []byte) []byte {
	result := []byte{version, byte(flags >> 16), byte(flags >> 8), byte(flags)}
	return append(result, payload...)
}

func fixtureU32(value uint32) []byte {
	var result [4]byte
	binary.BigEndian.PutUint32(result[:], value)
	return result[:]
}

func fixtureU64(value uint64) []byte {
	var result [8]byte
	binary.BigEndian.PutUint64(result[:], value)
	return result[:]
}

func manySampleMovie(count uint32) []byte {
	fileType := fixtureFileType("isom", "iso2")
	buildMoov := func(offset uint64) []byte {
		stbl := fixtureContainer("stbl",
			fixtureSTSD("avc1"),
			fixtureSTTS([]fixtureTiming{{count: count, duration: 1}}),
			fixtureSTSC([]fixtureChunk{{first: 1, samples: count, description: 1}}),
			fixtureSTSZCount(1, count),
			fixtureSTCOValues([]uint64{offset}),
		)
		return fixtureContainer("moov", fixtureMVHD(), fixtureTrackWithTable(1, 1_000, "vide", stbl))
	}
	moov := buildMoov(0)
	mediaStart := uint64(len(fileType) + len(moov) + 8)
	moov = buildMoov(mediaStart)
	return append(append(fileType, moov...), fixtureBox("mdat", make([]byte, int(count)))...)
}

func manyTimingMovie(count uint32) []byte {
	timing := make([]fixtureTiming, count)
	for index := range timing {
		timing[index] = fixtureTiming{count: 1, duration: 1}
	}
	fileType := fixtureFileType("isom", "iso2")
	buildMoov := func(offset uint64) []byte {
		stbl := fixtureContainer("stbl",
			fixtureSTSD("avc1"),
			fixtureSTTS(timing),
			fixtureSTSC([]fixtureChunk{{first: 1, samples: count, description: 1}}),
			fixtureSTSZCount(1, count),
			fixtureSTCOValues([]uint64{offset}),
		)
		return fixtureContainer("moov", fixtureMVHD(), fixtureTrackWithTable(1, 1_000, "vide", stbl))
	}
	moov := buildMoov(0)
	mediaStart := uint64(len(fileType) + len(moov) + 8)
	moov = buildMoov(mediaStart)
	return append(append(fileType, moov...), fixtureBox("mdat", make([]byte, int(count)))...)
}

func mixedTableMovie(largeOffsets bool) ([]byte, []sample) {
	fileType := fixtureFileType("isom", "iso2")
	buildMoov := func(first, second uint64) []byte {
		offsets := fixtureSTCOValues([]uint64{first, second})
		if largeOffsets {
			offsets = fixtureCO64Values([]uint64{first, second})
		}
		stbl := fixtureContainer("stbl",
			fixtureSTSD("avc1"),
			fixtureSTTS([]fixtureTiming{{count: 2, duration: 10}, {count: 2, duration: 20}}),
			fixtureCTTSRuns([]fixtureCompositionRun{{count: 1, offset: -2}, {count: 2, offset: 3}, {count: 1, offset: 0}}),
			fixtureSTSC([]fixtureChunk{{first: 1, samples: 2, description: 1}, {first: 2, samples: 2, description: 1}}),
			fixtureSTSZValues([]uint32{2, 3, 4, 5}),
			offsets,
			fixtureSTSS([]uint32{1, 3}),
		)
		return fixtureContainer("moov", fixtureMVHD(), fixtureTrackWithTable(1, 1_000, "vide", stbl))
	}
	moov := buildMoov(0, 0)
	first := uint64(len(fileType) + len(moov) + 8)
	second := first + 5
	moov = buildMoov(first, second)
	data := append(append(fileType, moov...), fixtureBox("mdat", make([]byte, 14))...)
	return data, []sample{
		{offset: first, size: 2, duration: 10, dts: 0, pts: -2, descriptionIndex: 1, sync: true, sequence: 1},
		{offset: first + 2, size: 3, duration: 10, dts: 10, pts: 13, descriptionIndex: 1, sequence: 2},
		{offset: second, size: 4, duration: 20, dts: 20, pts: 23, descriptionIndex: 1, sync: true, sequence: 3},
		{offset: second + 4, size: 5, duration: 20, dts: 40, pts: 40, descriptionIndex: 1, sequence: 4},
	}
}

func fixtureTrackWithTable(id, timeScale uint32, handler string, stbl []byte) []byte {
	mdhd := make([]byte, 24)
	binary.BigEndian.PutUint32(mdhd[12:16], timeScale)
	hdlr := make([]byte, 24)
	copy(hdlr[8:12], handler)
	mediaHeader := fixtureBox("smhd", make([]byte, 8))
	if handler == "vide" {
		mediaHeader = fixtureBox("vmhd", make([]byte, 12))
	}
	minf := fixtureContainer("minf", mediaHeader, fixtureDINF(), stbl)
	return fixtureContainer("trak", fixtureTKHD(id), fixtureContainer("mdia", fixtureBox("mdhd", mdhd), fixtureBox("hdlr", hdlr), minf))
}

func fixtureSTSZCount(size, count uint32) []byte {
	payload := fixtureFullBox(0, 0, fixtureU32(size))
	payload = append(payload, fixtureU32(count)...)
	return fixtureBox("stsz", payload)
}

func fixtureSTSZValues(values []uint32) []byte {
	payload := fixtureFullBox(0, 0, fixtureU32(0))
	payload = append(payload, fixtureU32(uint32(len(values)))...)
	for _, value := range values {
		payload = append(payload, fixtureU32(value)...)
	}
	return fixtureBox("stsz", payload)
}

func fixtureSTCOValues(values []uint64) []byte {
	payload := fixtureFullBox(0, 0, fixtureU32(uint32(len(values))))
	for _, value := range values {
		payload = append(payload, fixtureU32(uint32(value))...)
	}
	return fixtureBox("stco", payload)
}

func fixtureCO64Values(values []uint64) []byte {
	payload := fixtureFullBox(0, 0, fixtureU32(uint32(len(values))))
	for _, value := range values {
		payload = append(payload, fixtureU64(value)...)
	}
	return fixtureBox("co64", payload)
}

type fixtureCompositionRun struct {
	count  uint32
	offset int32
}

func fixtureCTTSRuns(values []fixtureCompositionRun) []byte {
	payload := fixtureFullBox(1, 0, fixtureU32(uint32(len(values))))
	for _, value := range values {
		payload = append(payload, fixtureU32(value.count)...)
		payload = append(payload, fixtureU32(uint32(value.offset))...)
	}
	return fixtureBox("ctts", payload)
}
