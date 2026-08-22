package integration_test

import "encoding/binary"

type mp4FixtureTrack struct {
	id          uint32
	timeScale   uint32
	handler     string
	entry       string
	duration    uint32
	composition int32
	payload     []byte
	// channels is non-zero for a sample entry that carries the AudioSampleEntry
	// fields, so the track describes 16-bit linear PCM at timeScale.
	channels uint16
	// samples repeats payload that many times. Zero means one sample, so a
	// track that only needs a single payload says nothing.
	samples uint32
	// chunks splits those samples into that many equal chunks. Zero means one
	// chunk. A movie an encoder actually produced stores several chunks per
	// track and interleaves them, so a table that describes one chunk per track
	// never reaches the geometry a player reads.
	chunks uint32
	// offsets holds the stored position of each chunk, filled in during layout.
	offsets []uint32
}

func (t mp4FixtureTrack) sampleCount() uint32 {
	if t.samples == 0 {
		return 1
	}
	return t.samples
}

func (t mp4FixtureTrack) chunkCount() uint32 {
	if t.chunks == 0 {
		return 1
	}
	return t.chunks
}

func (t mp4FixtureTrack) samplesPerChunk() uint32 {
	return t.sampleCount() / t.chunkCount()
}

func (t mp4FixtureTrack) chunkBytes() uint64 {
	return uint64(t.samplesPerChunk()) * uint64(len(t.payload))
}

func (t mp4FixtureTrack) mediaBytes() uint64 {
	return uint64(t.sampleCount()) * uint64(len(t.payload))
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

// mp4FixtureChunk names one chunk of one track in stored order.
type mp4FixtureChunk struct {
	track int
	index uint32
}

func mp4Fixture(tracks []mp4FixtureTrack) []byte {
	return mp4FixtureStored(tracks, mp4FixtureTrackMajor(tracks, nil))
}

// mp4ExternalOffsetFixture carries a top-level sidx, which records byte offsets
// the sample tables know nothing about. Such a movie survives only a remux that
// puts every byte back where it was.
func mp4ExternalOffsetFixture(chunks, samplesPerChunk uint32) []byte {
	tracks := []mp4FixtureTrack{
		{id: 1, timeScale: 1_000, handler: "vide", entry: "avc1", duration: 40, payload: []byte{0xde, 0xad}, samples: chunks * samplesPerChunk, chunks: chunks},
		{id: 2, timeScale: 48_000, handler: "soun", entry: "zzzz", duration: 1024, payload: []byte{0xca, 0xfe, 0xba}, samples: chunks * samplesPerChunk, chunks: chunks},
	}
	return mp4FixtureStored(tracks, mp4FixtureInterleave(tracks), mp4FixtureBox("sidx", make([]byte, 12)))
}

// mp4FixtureOrdered stores whole tracks one after another in the given track
// order. A nil order is track order; any other permutation makes the stored
// order disagree with the order the tracks are described in.
func mp4FixtureOrdered(tracks []mp4FixtureTrack, order []int) []byte {
	return mp4FixtureStored(tracks, mp4FixtureTrackMajor(tracks, order))
}

// mp4FixtureInterleaved stores one chunk from each track in turn, which is how
// an encoder lays a movie out so a player reading forward has both tracks to
// hand. Track-major storage is the shape a fixture reaches by accident; this is
// the shape files arrive in.
func mp4FixtureInterleaved(tracks []mp4FixtureTrack) []byte {
	return mp4FixtureStored(tracks, mp4FixtureInterleave(tracks))
}

func mp4FixtureInterleave(tracks []mp4FixtureTrack) []mp4FixtureChunk {
	var longest uint32
	for _, track := range tracks {
		longest = max(longest, track.chunkCount())
	}
	var placement []mp4FixtureChunk
	for index := uint32(0); index < longest; index++ {
		for position, track := range tracks {
			if index < track.chunkCount() {
				placement = append(placement, mp4FixtureChunk{track: position, index: index})
			}
		}
	}
	return placement
}

func mp4FixtureTrackMajor(tracks []mp4FixtureTrack, order []int) []mp4FixtureChunk {
	if order == nil {
		order = make([]int, len(tracks))
		for index := range tracks {
			order[index] = index
		}
	}
	var placement []mp4FixtureChunk
	for _, position := range order {
		for index := range tracks[position].chunkCount() {
			placement = append(placement, mp4FixtureChunk{track: position, index: index})
		}
	}
	return placement
}

// mp4FixtureStored writes the movie with its chunks laid out in placement
// order. The chunk-offset tables are sized before the layout runs so that
// filling them in does not move the media.
func mp4FixtureStored(tracks []mp4FixtureTrack, placement []mp4FixtureChunk, topLevel ...[]byte) []byte {
	for index := range tracks {
		tracks[index].offsets = make([]uint32, tracks[index].chunkCount())
	}
	fileTypePayload := append([]byte("isom"), mp4FixtureU32(0)...)
	fileTypePayload = append(fileTypePayload, []byte("iso2")...)
	fileType := mp4FixtureBox("ftyp", fileTypePayload)
	for _, box := range topLevel {
		fileType = append(fileType, box...)
	}
	moov := mp4FixtureMoov(tracks)
	mediaStart := uint64(len(fileType) + len(moov) + 8)
	position := mediaStart
	for _, chunk := range placement {
		tracks[chunk.track].offsets[chunk.index] = uint32(position)
		position += tracks[chunk.track].chunkBytes()
	}
	moov = mp4FixtureMoov(tracks)
	media := make([]byte, 0, position-mediaStart)
	for _, chunk := range placement {
		for range tracks[chunk.track].samplesPerChunk() {
			media = append(media, tracks[chunk.track].payload...)
		}
	}
	return append(append(fileType, moov...), mp4FixtureBox("mdat", media)...)
}

// mp4StoredOutOfOrderFixture stores the second track's sample before the first,
// so a remux that follows the described track order has to move every sample.
func mp4StoredOutOfOrderFixture() []byte {
	return mp4FixtureOrdered([]mp4FixtureTrack{
		{id: 1, timeScale: 48_000, handler: "soun", entry: "zzzz", duration: 1024, payload: []byte{0xde, 0xad}},
		{id: 2, timeScale: 1_000, handler: "vide", entry: "avc1", duration: 40, composition: 3, payload: []byte{0xca, 0xfe, 0xba}},
	}, []int{1, 0})
}

// mp4InterleavedFixture is the geometry a real movie has: two tracks, several
// samples each, split into chunks that alternate through mdat.
func mp4InterleavedFixture(chunks, samplesPerChunk uint32) []byte {
	return mp4FixtureInterleaved([]mp4FixtureTrack{
		{id: 1, timeScale: 1_000, handler: "vide", entry: "avc1", duration: 40, payload: []byte{0xde, 0xad}, samples: chunks * samplesPerChunk, chunks: chunks},
		{id: 2, timeScale: 48_000, handler: "soun", entry: "zzzz", duration: 1024, payload: []byte{0xca, 0xfe, 0xba}, samples: chunks * samplesPerChunk, chunks: chunks},
	})
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
				mp4FixtureSTTS(track.duration, track.sampleCount()),
				mp4FixtureSTSC(track.samplesPerChunk()),
				mp4FixtureSTSZ(uint32(len(track.payload)), track.sampleCount()),
				mp4FixtureSTCO(track.offsets),
				mp4FixtureCTTS(track.composition, track.sampleCount()),
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

func mp4FixtureSTTS(duration, samples uint32) []byte {
	return mp4FixtureBox("stts", mp4FixtureTable(1, mp4FixtureU32(samples), mp4FixtureU32(duration)))
}

func mp4FixtureSTSC(samplesPerChunk uint32) []byte {
	return mp4FixtureBox("stsc", mp4FixtureTable(1, mp4FixtureU32(1), mp4FixtureU32(samplesPerChunk), mp4FixtureU32(1)))
}

func mp4FixtureSTSZ(size, samples uint32) []byte {
	return mp4FixtureBox("stsz", append(mp4FixtureFullBox(0, 0, mp4FixtureU32(size)), mp4FixtureU32(samples)...))
}

func mp4FixtureSTCO(offsets []uint32) []byte {
	rows := make([][]byte, len(offsets))
	for index, offset := range offsets {
		rows[index] = mp4FixtureU32(offset)
	}
	return mp4FixtureBox("stco", mp4FixtureTable(uint32(len(offsets)), rows...))
}

func mp4FixtureCTTS(offset int32, samples uint32) []byte {
	if offset == 0 {
		return nil
	}
	payload := mp4FixtureFullBox(1, 0, mp4FixtureU32(1))
	payload = append(payload, mp4FixtureU32(samples)...)
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

// mp4ManySampleFixture is one track of count single-byte samples in a single
// chunk. Every sample table stays a fixed size, so anything that grows with the
// sample count is the reader's own state rather than the file's.
func mp4ManySampleFixture(count uint32) []byte {
	return mp4Fixture([]mp4FixtureTrack{
		{id: 1, timeScale: 1_000, handler: "vide", entry: "avc1", duration: 1, payload: []byte{0}, samples: count},
	})
}

// mp4ManyChunkFixture gives every sample its own chunk, so the chunk-offset
// table -- and the journal that rebuilds it -- grows with the movie while every
// in-memory table stays the same size.
func mp4ManyChunkFixture(chunks uint32) []byte {
	return mp4Fixture([]mp4FixtureTrack{
		{id: 1, timeScale: 1_000, handler: "vide", entry: "avc1", duration: 1, payload: []byte{0}, samples: chunks, chunks: chunks},
	})
}

// mp4ManySampleTwoTrackFixture is the ordinary shape of a real movie: more than
// one track, and more than one sample in each. A remux only stays correct here
// while every sample of a track reaches the muxer before the next track starts,
// so this is the fixture that notices when the routes are reordered on the way.
func mp4ManySampleTwoTrackFixture(count uint32) []byte {
	return mp4Fixture([]mp4FixtureTrack{
		{id: 1, timeScale: 1_000, handler: "vide", entry: "avc1", duration: 40, payload: []byte{0xde, 0xad}, samples: count},
		{id: 2, timeScale: 48_000, handler: "soun", entry: "zzzz", duration: 1024, payload: []byte{0xca, 0xfe, 0xba}, samples: count},
	})
}

// mp4FixtureTable writes a full box holding an entry count and its rows.
func mp4FixtureTable(entries uint32, rows ...[]byte) []byte {
	payload := mp4FixtureFullBox(0, 0, mp4FixtureU32(entries))
	for _, row := range rows {
		payload = append(payload, row...)
	}
	return payload
}

// mp4EditedPCMFixture is a decodable little-endian PCM track that also carries
// an empty edts. Nothing else about the movie changes, so a test that fails on
// it is reacting to the edit list alone.
func mp4EditedPCMFixture(payload []byte) []byte {
	value := mp4PCMFixture("sowt", payload)
	edts := mp4FixtureContainer("edts", mp4FixtureBox("elst", mp4FixtureFullBox(0, 0, mp4FixtureU32(0))))
	return mp4FixtureInsertIntoTrack(value, edts)
}

// mp4FixtureInsertIntoTrack appends a box to the end of the first trak and
// repairs every enclosing box length. Offsets inside mdat move by the same
// amount, so the chunk-offset table is patched too.
func mp4FixtureInsertIntoTrack(value, box []byte) []byte {
	top := mp4FixtureTopLevel(value)
	var moov, trak mp4FixtureBoxView
	for _, candidate := range top {
		if candidate.typeID == "moov" {
			moov = candidate
		}
	}
	for _, child := range mp4FixtureChildren(moov.payload, 0, len(moov.payload)) {
		if child.typeID == "trak" {
			trak = child
			break
		}
	}
	trakEnd := moov.start + 8 + trak.start + 8 + len(trak.payload)
	result := make([]byte, 0, len(value)+len(box))
	result = append(result, value[:trakEnd]...)
	result = append(result, box...)
	result = append(result, value[trakEnd:]...)

	grow := uint32(len(box))
	mp4FixtureGrowBox(result, moov.start, grow)
	mp4FixtureGrowBox(result, moov.start+8+trak.start, grow)
	for _, offset := range mp4FixtureChunkOffsetFields(result) {
		binary.BigEndian.PutUint32(result[offset:], binary.BigEndian.Uint32(result[offset:])+grow)
	}
	return result
}

func mp4FixtureGrowBox(value []byte, start int, grow uint32) {
	binary.BigEndian.PutUint32(value[start:], binary.BigEndian.Uint32(value[start:])+grow)
}

// mp4FixtureChunkOffsetFields locates every stco entry in the file so a fixture
// that shifts mdat can keep the tables consistent.
func mp4FixtureChunkOffsetFields(value []byte) []int {
	var result []int
	var walk func(payload []byte, base int)
	walk = func(payload []byte, base int) {
		for _, child := range mp4FixtureChildren(payload, 0, len(payload)) {
			start := base + child.start
			if child.typeID == "stco" {
				for index := 8; index+4 <= len(child.payload); index += 4 {
					result = append(result, start+8+index)
				}
				continue
			}
			switch child.typeID {
			case "moov", "trak", "mdia", "minf", "stbl", "edts":
				walk(child.payload, start+8)
			}
		}
	}
	walk(value, 0)
	return result
}
