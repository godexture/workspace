package mp4

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/godexture/godec/access"
)

func parseMoov(ctx context.Context, reader access.Random, sourceEnd uint64, value box, budget *movieBudget) ([]track, box, error) {
	var movieHead box
	var haveHeader bool
	var trackCount uint64
	err := scanChildBoxes(ctx, reader, sourceEnd, value, func(child box) error {
		switch child.typeID {
		case typeMVHD:
			if haveHeader {
				return fmt.Errorf("%w: mvhd is repeated", errMalformedMovie)
			}
			if err := validateMovieHeader(ctx, reader, child); err != nil {
				return err
			}
			movieHead = child
			haveHeader = true
		case typeTRAK:
			if trackCount == ^uint64(0) {
				return fmt.Errorf("%w: track count overflows", errMalformedMovie)
			}
			trackCount++
		case typeMVEX, typeMOOF:
			return fmt.Errorf("%w: fragmented movie boxes are not supported", errUnsupportedMovie)
		case typeEDTS, typeELST:
			return fmt.Errorf("%w: edit lists are not supported", errUnsupportedMovie)
		}
		return nil
	})
	if err != nil {
		return nil, box{}, err
	}
	if !haveHeader || trackCount == 0 {
		return nil, box{}, fmt.Errorf("%w: moov requires mvhd and one trak", errMalformedMovie)
	}
	if trackCount > uint64(^uint(0)>>1) {
		return nil, box{}, fmt.Errorf("%w: track count exceeds runtime range", errUnsupportedMovie)
	}
	if err := budget.reserveTracks(trackCount); err != nil {
		return nil, box{}, err
	}
	tracks := make([]track, 0, int(trackCount))
	trackIDs := make(map[uint32]struct{}, trackCount)
	err = scanChildBoxes(ctx, reader, sourceEnd, value, func(child box) error {
		if child.typeID != typeTRAK {
			return nil
		}
		parsed, err := parseTrack(ctx, reader, sourceEnd, child)
		if err != nil {
			return err
		}
		if _, exists := trackIDs[parsed.id]; exists {
			return fmt.Errorf("%w: track ID %d is repeated", errMalformedMovie, parsed.id)
		}
		trackIDs[parsed.id] = struct{}{}
		tracks = append(tracks, parsed)
		return nil
	})
	if err != nil {
		return nil, box{}, err
	}
	return tracks, movieHead, nil
}

func parseTrack(ctx context.Context, reader access.Random, sourceEnd uint64, value box) (track, error) {
	result := track{trak: value}
	var haveHeader, haveMedia bool
	err := scanChildBoxes(ctx, reader, sourceEnd, value, func(child box) error {
		switch child.typeID {
		case typeTKHD:
			if haveHeader {
				return fmt.Errorf("%w: tkhd is repeated", errMalformedMovie)
			}
			id, err := readTrackID(ctx, reader, child)
			if err != nil {
				return err
			}
			result.id = id
			result.trackHead = child
			haveHeader = true
		case typeMDIA:
			if haveMedia {
				return fmt.Errorf("%w: mdia is repeated", errMalformedMovie)
			}
			if err := parseMedia(ctx, reader, sourceEnd, child, &result); err != nil {
				return err
			}
			haveMedia = true
		case typeEDTS:
		case typeELST:
			return fmt.Errorf("%w: elst outside edts", errUnsupportedMovie)
		}
		return nil
	})
	if err != nil {
		return track{}, err
	}
	if !haveHeader || !haveMedia || result.id == 0 {
		return track{}, fmt.Errorf("%w: trak requires tkhd and mdia", errMalformedMovie)
	}
	return result, nil
}

func readTrackID(ctx context.Context, reader access.Random, value box) (uint32, error) {
	var prefix [96]byte
	if err := readBoxPrefix(ctx, reader, value, prefix[:1], "tkhd"); err != nil {
		return 0, err
	}
	length := 84
	if prefix[0] == 1 {
		length = 96
	}
	if err := readBoxPrefix(ctx, reader, value, prefix[:length], "tkhd"); err != nil {
		return 0, err
	}
	return parseTrackID(prefix[:length])
}

func parseTrackID(data []byte) (uint32, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("%w: tkhd has no version", errMalformedMovie)
	}
	switch data[0] {
	case 0:
		if len(data) < 84 {
			return 0, fmt.Errorf("%w: tkhd version zero is shorter than 84 bytes", errMalformedMovie)
		}
		id := binary.BigEndian.Uint32(data[12:16])
		if id == 0 {
			return 0, fmt.Errorf("%w: tkhd track ID is zero", errMalformedMovie)
		}
		return id, nil
	case 1:
		if len(data) < 96 {
			return 0, fmt.Errorf("%w: tkhd version one is shorter than 96 bytes", errMalformedMovie)
		}
		id := binary.BigEndian.Uint32(data[20:24])
		if id == 0 {
			return 0, fmt.Errorf("%w: tkhd track ID is zero", errMalformedMovie)
		}
		return id, nil
	default:
		return 0, fmt.Errorf("%w: tkhd version %d", errUnsupportedMovie, data[0])
	}
}

func parseMedia(ctx context.Context, reader access.Random, sourceEnd uint64, value box, result *track) error {
	result.media = value
	var haveHeader, haveHandler, haveInfo bool
	err := scanChildBoxes(ctx, reader, sourceEnd, value, func(child box) error {
		switch child.typeID {
		case typeMDHD:
			if haveHeader {
				return fmt.Errorf("%w: mdhd is repeated", errMalformedMovie)
			}
			timeScale, err := readTimeScale(ctx, reader, child)
			if err != nil {
				return err
			}
			result.timeScale = timeScale
			result.mediaHead = child
			haveHeader = true
		case typeHDLR:
			if haveHandler {
				return fmt.Errorf("%w: hdlr is repeated", errMalformedMovie)
			}
			handler, err := readHandler(ctx, reader, child)
			if err != nil {
				return err
			}
			result.handler = handler
			result.handlerBox = child
			haveHandler = true
		case typeMINF:
			if haveInfo {
				return fmt.Errorf("%w: minf is repeated", errMalformedMovie)
			}
			if err := parseMediaInfo(ctx, reader, sourceEnd, child, result); err != nil {
				return err
			}
			haveInfo = true
		case typeEDTS, typeELST:
			return fmt.Errorf("%w: edit lists are not supported", errUnsupportedMovie)
		default:
			return fmt.Errorf("%w: mdia child %q", errUnsupportedMovie, child.typeID)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !haveHeader || !haveHandler || !haveInfo {
		return fmt.Errorf("%w: mdia requires mdhd, hdlr, and minf", errMalformedMovie)
	}
	return nil
}

func readTimeScale(ctx context.Context, reader access.Random, value box) (uint32, error) {
	var prefix [36]byte
	if err := readBoxPrefix(ctx, reader, value, prefix[:1], "mdhd"); err != nil {
		return 0, err
	}
	length := 24
	if prefix[0] == 1 {
		length = 36
	}
	if err := readBoxPrefix(ctx, reader, value, prefix[:length], "mdhd"); err != nil {
		return 0, err
	}
	return parseTimeScale(prefix[:length])
}

func parseTimeScale(data []byte) (uint32, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("%w: mdhd has no version", errMalformedMovie)
	}
	var timeScale uint32
	switch data[0] {
	case 0:
		if len(data) < 24 {
			return 0, fmt.Errorf("%w: mdhd version zero is shorter than 24 bytes", errMalformedMovie)
		}
		timeScale = binary.BigEndian.Uint32(data[12:16])
	case 1:
		if len(data) < 36 {
			return 0, fmt.Errorf("%w: mdhd version one is shorter than 36 bytes", errMalformedMovie)
		}
		timeScale = binary.BigEndian.Uint32(data[20:24])
	default:
		return 0, fmt.Errorf("%w: mdhd version %d", errUnsupportedMovie, data[0])
	}
	if timeScale == 0 {
		return 0, fmt.Errorf("%w: mdhd timescale is zero", errMalformedMovie)
	}
	return timeScale, nil
}

func readHandler(ctx context.Context, reader access.Random, value box) (boxType, error) {
	var prefix [24]byte
	if err := readBoxPrefix(ctx, reader, value, prefix[:], "hdlr"); err != nil {
		return boxType{}, err
	}
	return parseHandler(prefix[:])
}

func parseHandler(data []byte) (boxType, error) {
	if len(data) < 24 {
		return boxType{}, fmt.Errorf("%w: hdlr is shorter than 24 bytes", errMalformedMovie)
	}
	if data[0] != 0 {
		return boxType{}, fmt.Errorf("%w: hdlr version %d", errUnsupportedMovie, data[0])
	}
	return boxType(data[8:12]), nil
}

func parseMediaInfo(ctx context.Context, reader access.Random, sourceEnd uint64, value box, result *track) error {
	result.mediaInfo = value
	var haveHeader, haveDataInfo, haveSampleTable bool
	var dataInfo, sampleTable box
	err := scanChildBoxes(ctx, reader, sourceEnd, value, func(child box) error {
		switch child.typeID {
		case typeVMHD, typeSMHD, typeHMHD, typeNMHD, typeGMHD:
			if haveHeader {
				return fmt.Errorf("%w: minf media header is repeated", errMalformedMovie)
			}
			result.mediaInfo = child
			haveHeader = true
		case typeDINF:
			if haveDataInfo {
				return fmt.Errorf("%w: dinf is repeated", errMalformedMovie)
			}
			dataInfo = child
			haveDataInfo = true
		case typeSTBL:
			if haveSampleTable {
				return fmt.Errorf("%w: stbl is repeated", errMalformedMovie)
			}
			sampleTable = child
			haveSampleTable = true
		default:
			return fmt.Errorf("%w: minf child %q", errUnsupportedMovie, child.typeID)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !haveHeader || !haveDataInfo || !haveSampleTable {
		return fmt.Errorf("%w: minf requires media header, dinf, and stbl", errMalformedMovie)
	}
	count, err := parseDataInfo(ctx, reader, sourceEnd, dataInfo)
	if err != nil {
		return err
	}
	result.dataInfo = dataInfo
	result.dataReferences = count
	result.sampleBox = sampleTable
	return parseSampleTable(ctx, reader, sourceEnd, sampleTable, result, count)
}

func parseDataInfo(ctx context.Context, reader access.Random, sourceEnd uint64, value box) (uint32, error) {
	var count uint32
	var found bool
	err := scanChildBoxes(ctx, reader, sourceEnd, value, func(child box) error {
		if child.typeID != typeDREF {
			return fmt.Errorf("%w: dinf child %q", errUnsupportedMovie, child.typeID)
		}
		if found {
			return fmt.Errorf("%w: dref is repeated", errMalformedMovie)
		}
		parsed, err := parseDataReferences(ctx, reader, sourceEnd, child)
		if err != nil {
			return err
		}
		count = parsed
		found = true
		return nil
	})
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, fmt.Errorf("%w: dinf has no dref", errMalformedMovie)
	}
	return count, nil
}

func parseDataReferences(ctx context.Context, reader access.Random, sourceEnd uint64, value box) (uint32, error) {
	var prefix [8]byte
	if err := readBoxPrefix(ctx, reader, value, prefix[:], "dref"); err != nil {
		return 0, err
	}
	if prefix[0] != 0 || prefix[1] != 0 || prefix[2] != 0 || prefix[3] != 0 {
		return 0, fmt.Errorf("%w: dref full-box header", errUnsupportedMovie)
	}
	want := binary.BigEndian.Uint32(prefix[4:])
	if want == 0 {
		return 0, fmt.Errorf("%w: dref has no entries", errMalformedMovie)
	}
	start, ok := checkedBoxAdd(value.payloadOffset, 8)
	end, endOK := payloadEnd(value)
	if !ok || !endOK || start > end {
		return 0, fmt.Errorf("%w: dref entry range", errMalformedMovie)
	}
	var found uint32
	err := scanBoxes(ctx, reader, boxScope{sourceEnd: sourceEnd, start: start, end: end}, func(entry box) error {
		if entry.typeID == typeURN {
			return fmt.Errorf("%w: urn data references are external", errUnsupportedMovie)
		}
		if entry.typeID != typeURL {
			return fmt.Errorf("%w: data reference %q", errUnsupportedMovie, entry.typeID)
		}
		var full [4]byte
		if err := readBoxPrefix(ctx, reader, entry, full[:], "url data reference"); err != nil {
			return err
		}
		flags := uint32(full[1])<<16 | uint32(full[2])<<8 | uint32(full[3])
		if full[0] != 0 || flags&1 == 0 || entry.payloadSize != 4 {
			return fmt.Errorf("%w: external url data reference", errUnsupportedMovie)
		}
		if found == ^uint32(0) {
			return fmt.Errorf("%w: dref entry count overflows", errMalformedMovie)
		}
		found++
		return nil
	})
	if err != nil {
		return 0, err
	}
	if found != want {
		return 0, fmt.Errorf("%w: dref count is %d, found %d", errMalformedMovie, want, found)
	}
	return found, nil
}
