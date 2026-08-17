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
