package integration_test

import "encoding/binary"

type mp4FixtureTrack struct {
	id          uint32
	timeScale   uint32
	handler     string
	entry       string
	duration    uint32
	composition int32
	offset      int32
	payload     []byte
	// channels is non-zero for a sample entry that carries the AudioSampleEntry
	// fields, so the track describes 16-bit linear PCM at timeScale.
	channels uint16
}

func mp4TwoTrackFixture() []byte {
	return mp4Fixture([]mp4FixtureTrack{
		{id: 1, timeScale: 48_000, handler: "soun", entry: "zzzz", duration: 1024, payload: []byte{0xde, 0xad}},
		{id: 2, timeScale: 1_000, handler: "vide", entry: "avc1", duration: 40, composition: 3, payload: []byte{0xca, 0xfe, 0xba}},
	})
}

// mp4PCMFixture builds a one-track MP4 whose sample entry describes 16-bit
// stereo linear PCM at 48 kHz. sowt is little-endian and twos is big-endian.
func mp4PCMFixture(entry string, payload []byte) []byte {
	return mp4Fixture([]mp4FixtureTrack{
		{id: 1, timeScale: 48_000, handler: "soun", entry: entry, duration: uint32(len(payload) / 4), payload: payload, channels: 2},
	})
}

func mp4Fixture(tracks []mp4FixtureTrack) []byte {
	return mp4FixtureOrdered(tracks, nil)
}

// mp4FixtureOrdered lays the sample payloads out in mdat order. A nil order is
// track order; any other permutation makes the stored order disagree with the
// order a remux emits its routes in.
func mp4FixtureOrdered(tracks []mp4FixtureTrack, order []int) []byte {
	if order == nil {
		order = make([]int, len(tracks))
		for index := range tracks {
			order[index] = index
		}
	}
	fileTypePayload := append([]byte("isom"), mp4FixtureU32(0)...)
	fileTypePayload = append(fileTypePayload, []byte("iso2")...)
	fileType := mp4FixtureBox("ftyp", fileTypePayload)
	moov := mp4FixtureMoov(tracks)
	mediaStart := uint64(len(fileType) + len(moov) + 8)
	position := mediaStart
	for _, index := range order {
		tracks[index].offset = int32(position)
		position += uint64(len(tracks[index].payload))
	}
	moov = mp4FixtureMoov(tracks)
	media := make([]byte, 0, position-mediaStart)
	for _, index := range order {
		media = append(media, tracks[index].payload...)
	}
	return append(append(fileType, moov...), mp4FixtureBox("mdat", media)...)
}

// mp4StoredOutOfOrderFixture stores the second track's sample before the first,
// so every sample moves when a remux writes them in route order.
func mp4StoredOutOfOrderFixture() []byte {
	return mp4FixtureOrdered([]mp4FixtureTrack{
		{id: 1, timeScale: 48_000, handler: "soun", entry: "zzzz", duration: 1024, payload: []byte{0xde, 0xad}},
		{id: 2, timeScale: 1_000, handler: "vide", entry: "avc1", duration: 40, composition: 3, payload: []byte{0xca, 0xfe, 0xba}},
	}, []int{1, 0})
}

func mp4FixtureMoov(tracks []mp4FixtureTrack) []byte {
	children := [][]byte{mp4FixtureMVHD()}
	for _, track := range tracks {
		children = append(children, mp4FixtureTrackBox(track))
	}
	return mp4FixtureContainer("moov", children...)
}

func mp4FixtureMVHD() []byte {
	payload := make([]byte, 100)
	binary.BigEndian.PutUint32(payload[12:16], 1_000)
	return mp4FixtureBox("mvhd", payload)
}

func mp4FixtureTrackBox(track mp4FixtureTrack) []byte {
	tkhd := make([]byte, 84)
	binary.BigEndian.PutUint32(tkhd[12:16], track.id)
	mdhd := make([]byte, 24)
	binary.BigEndian.PutUint32(mdhd[12:16], track.timeScale)
	hdlr := make([]byte, 24)
	copy(hdlr[8:12], track.handler)
	mediaHeader := mp4FixtureBox("smhd", make([]byte, 8))
	if track.handler == "vide" {
		mediaHeader = mp4FixtureBox("vmhd", make([]byte, 12))
	}
	return mp4FixtureContainer("trak",
		mp4FixtureBox("tkhd", tkhd),
		mp4FixtureContainer("mdia",
			mp4FixtureBox("mdhd", mdhd),
			mp4FixtureBox("hdlr", hdlr),
			mp4FixtureContainer("minf", mediaHeader, mp4FixtureDINF(), mp4FixtureContainer("stbl",
				mp4FixtureSTSD(track),
				mp4FixtureSTTS(track.duration),
				mp4FixtureSTSC(),
				mp4FixtureSTSZ(uint32(len(track.payload))),
				mp4FixtureSTCO(uint32(track.offset)),
				mp4FixtureCTTS(track.composition),
			)),
		),
	)
}

func mp4FixtureDINF() []byte {
	url := mp4FixtureBox("url ", mp4FixtureFullBox(0, 1, nil))
	dref := append(mp4FixtureFullBox(0, 0, mp4FixtureU32(1)), url...)
	return mp4FixtureContainer("dinf", mp4FixtureBox("dref", dref))
}

func mp4FixtureSTSD(track mp4FixtureTrack) []byte {
	entry := make([]byte, 16)
	if track.channels != 0 {
		entry = make([]byte, 36)
		binary.BigEndian.PutUint16(entry[24:26], track.channels)
		binary.BigEndian.PutUint16(entry[26:28], 16)
		binary.BigEndian.PutUint32(entry[32:36], track.timeScale<<16)
	}
	binary.BigEndian.PutUint32(entry[:4], uint32(len(entry)))
	copy(entry[4:8], track.entry)
	binary.BigEndian.PutUint16(entry[14:16], 1)
	payload := append(mp4FixtureFullBox(0, 0, mp4FixtureU32(1)), entry...)
	return mp4FixtureBox("stsd", payload)
}

func mp4FixtureSTTS(duration uint32) []byte {
	payload := mp4FixtureFullBox(0, 0, mp4FixtureU32(1))
	payload = append(payload, mp4FixtureU32(1)...)
	payload = append(payload, mp4FixtureU32(duration)...)
	return mp4FixtureBox("stts", payload)
}

func mp4FixtureSTSC() []byte {
	payload := mp4FixtureFullBox(0, 0, mp4FixtureU32(1))
	payload = append(payload, mp4FixtureU32(1)...)
	payload = append(payload, mp4FixtureU32(1)...)
	payload = append(payload, mp4FixtureU32(1)...)
	return mp4FixtureBox("stsc", payload)
}

func mp4FixtureSTSZ(size uint32) []byte {
	payload := mp4FixtureFullBox(0, 0, mp4FixtureU32(size))
	payload = append(payload, mp4FixtureU32(1)...)
	return mp4FixtureBox("stsz", payload)
}

func mp4FixtureSTCO(offset uint32) []byte {
	payload := mp4FixtureFullBox(0, 0, mp4FixtureU32(1))
	payload = append(payload, mp4FixtureU32(offset)...)
	return mp4FixtureBox("stco", payload)
}

func mp4FixtureCTTS(offset int32) []byte {
	if offset == 0 {
		return nil
	}
	payload := mp4FixtureFullBox(1, 0, mp4FixtureU32(1))
	payload = append(payload, mp4FixtureU32(1)...)
	payload = append(payload, mp4FixtureU32(uint32(offset))...)
	return mp4FixtureBox("ctts", payload)
}

func mp4FixtureContainer(typeID string, children ...[]byte) []byte {
	var payload []byte
	for _, child := range children {
		if child != nil {
			payload = append(payload, child...)
		}
	}
	return mp4FixtureBox(typeID, payload)
}

func mp4FixtureBox(typeID string, payload []byte) []byte {
	result := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(result[:4], uint32(len(result)))
	copy(result[4:8], typeID)
	copy(result[8:], payload)
	return result
}

func mp4FixtureFullBox(version byte, flags uint32, payload []byte) []byte {
	result := []byte{version, byte(flags >> 16), byte(flags >> 8), byte(flags)}
	return append(result, payload...)
}

func mp4FixtureU32(value uint32) []byte {
	result := make([]byte, 4)
	binary.BigEndian.PutUint32(result, value)
	return result
}
