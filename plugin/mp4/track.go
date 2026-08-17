package mp4

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/godexture/godec/access"
)

func parseMoov(ctx context.Context, reader access.Random, sourceEnd uint64, value box, budget *movieBudget) ([]track, []anchor, error) {
	var tracks []track
	var anchors []anchor
	trackIDs := make(map[uint32]struct{})
	var haveHeader bool
	err := scanChildBoxes(ctx, reader, sourceEnd, value, func(child box) error {
		switch child.typeID {
		case typeMVHD:
			if haveHeader {
				return fmt.Errorf("%w: mvhd is repeated", errMalformedMovie)
			}
			raw, err := preserveRaw(ctx, reader, child, budget, "mvhd")
			if err != nil {
				return err
			}
			data, err := rawPayload(raw, child, "mvhd")
			if err != nil {
				return err
			}
			if err := parseMovieHeader(data); err != nil {
				return err
			}
			anchors = append(anchors, anchor{typeID: child.typeID, index: 0, raw: raw})
			haveHeader = true
		case typeTRAK:
			if err := budget.reserve(trackBudgetBytes, "track model"); err != nil {
				return err
			}
			parsed, err := parseTrack(ctx, reader, sourceEnd, child, budget)
			if err != nil {
				return err
			}
			if _, exists := trackIDs[parsed.id]; exists {
				return fmt.Errorf("%w: track ID %d is repeated", errMalformedMovie, parsed.id)
			}
			trackIDs[parsed.id] = struct{}{}
			if err := budget.reserve(anchorBudgetBytes, "moov track anchor"); err != nil {
				return err
			}
			anchors = append(anchors, anchor{typeID: child.typeID, index: len(tracks)})
			tracks = append(tracks, parsed)
		case typeMVEX, typeMOOF:
			return fmt.Errorf("%w: fragmented movie boxes are not supported", errUnsupportedMovie)
		case typeEDTS, typeELST:
			return fmt.Errorf("%w: edit lists are not supported", errUnsupportedMovie)
		default:
			raw, err := preserveRaw(ctx, reader, child, budget, "moov child")
			if err != nil {
				return err
			}
			anchors = append(anchors, anchor{typeID: child.typeID, index: -1, raw: raw})
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if !haveHeader || len(tracks) == 0 {
		return nil, nil, fmt.Errorf("%w: moov requires mvhd and one trak", errMalformedMovie)
	}
	return tracks, anchors, nil
}

func parseTrack(ctx context.Context, reader access.Random, sourceEnd uint64, value box, budget *movieBudget) (track, error) {
	var result track
	var haveHeader, haveMedia bool
	err := scanChildBoxes(ctx, reader, sourceEnd, value, func(child box) error {
		switch child.typeID {
		case typeTKHD:
			if haveHeader {
				return fmt.Errorf("%w: tkhd is repeated", errMalformedMovie)
			}
			raw, err := readRawBox(ctx, reader, child, budget, "tkhd")
			if err != nil {
				return err
			}
			data, err := rawPayload(raw, child, "tkhd")
			if err != nil {
				return err
			}
			id, err := parseTrackID(data)
			if err != nil {
				return err
			}
			result.id = id
			result.trackHeader = raw
			if err := budget.reserve(anchorBudgetBytes, "trak anchor"); err != nil {
				return err
			}
			result.anchors = append(result.anchors, anchor{typeID: child.typeID, index: 0})
			haveHeader = true
		case typeMDIA:
			if haveMedia {
				return fmt.Errorf("%w: mdia is repeated", errMalformedMovie)
			}
			if err := parseMedia(ctx, reader, sourceEnd, child, budget, &result); err != nil {
				return err
			}
			if err := budget.reserve(anchorBudgetBytes, "trak anchor"); err != nil {
				return err
			}
			result.anchors = append(result.anchors, anchor{typeID: child.typeID, index: 0})
			haveMedia = true
		case typeEDTS:
			raw, err := preserveRaw(ctx, reader, child, budget, "edts")
			if err != nil {
				return err
			}
			result.anchors = append(result.anchors, anchor{typeID: child.typeID, index: -1, raw: raw})
		case typeELST:
			return fmt.Errorf("%w: elst outside edts", errUnsupportedMovie)
		default:
			raw, err := preserveRaw(ctx, reader, child, budget, "trak child")
			if err != nil {
				return err
			}
			result.anchors = append(result.anchors, anchor{typeID: child.typeID, index: -1, raw: raw})
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

func parseMedia(ctx context.Context, reader access.Random, sourceEnd uint64, value box, budget *movieBudget, result *track) error {
	var haveHeader, haveHandler, haveInfo bool
	err := scanChildBoxes(ctx, reader, sourceEnd, value, func(child box) error {
		switch child.typeID {
		case typeMDHD:
			if haveHeader {
				return fmt.Errorf("%w: mdhd is repeated", errMalformedMovie)
			}
			raw, err := readRawBox(ctx, reader, child, budget, "mdhd")
			if err != nil {
				return err
			}
			data, err := rawPayload(raw, child, "mdhd")
			if err != nil {
				return err
			}
			timeScale, err := parseTimeScale(data)
			if err != nil {
				return err
			}
			result.timeScale = timeScale
			result.mediaHeader = raw
			haveHeader = true
		case typeHDLR:
			if haveHandler {
				return fmt.Errorf("%w: hdlr is repeated", errMalformedMovie)
			}
			raw, err := readRawBox(ctx, reader, child, budget, "hdlr")
			if err != nil {
				return err
			}
			data, err := rawPayload(raw, child, "hdlr")
			if err != nil {
				return err
			}
			handler, err := parseHandler(data)
			if err != nil {
				return err
			}
			result.handler = handler
			result.handlerHeader = raw
			haveHandler = true
		case typeMINF:
			if haveInfo {
				return fmt.Errorf("%w: minf is repeated", errMalformedMovie)
			}
			if err := parseMediaInfo(ctx, reader, sourceEnd, child, budget, result); err != nil {
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

func parseHandler(data []byte) (boxType, error) {
	if len(data) < 24 {
		return boxType{}, fmt.Errorf("%w: hdlr is shorter than 24 bytes", errMalformedMovie)
	}
	if data[0] != 0 {
		return boxType{}, fmt.Errorf("%w: hdlr version %d", errUnsupportedMovie, data[0])
	}
	return boxType(data[8:12]), nil
}

func parseMediaInfo(ctx context.Context, reader access.Random, sourceEnd uint64, value box, budget *movieBudget, result *track) error {
	var haveHeader, haveDataInfo, haveSampleTable bool
	var dataInfo, sampleTable box
	err := scanChildBoxes(ctx, reader, sourceEnd, value, func(child box) error {
		switch child.typeID {
		case typeVMHD, typeSMHD, typeHMHD, typeNMHD, typeGMHD:
			if haveHeader {
				return fmt.Errorf("%w: minf media header is repeated", errMalformedMovie)
			}
			raw, err := readRawBox(ctx, reader, child, budget, "minf media header")
			if err != nil {
				return err
			}
			result.mediaInfo = raw
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
	count, raw, err := parseDataInfo(ctx, reader, sourceEnd, dataInfo, budget)
	if err != nil {
		return err
	}
	result.dataInfo = raw
	return parseSampleTable(ctx, reader, sourceEnd, sampleTable, budget, result, count)
}

func parseDataInfo(ctx context.Context, reader access.Random, sourceEnd uint64, value box, budget *movieBudget) (uint32, []byte, error) {
	raw, err := readRawBox(ctx, reader, value, budget, "dinf")
	if err != nil {
		return 0, nil, err
	}
	var count uint32
	var found bool
	err = scanChildBoxes(ctx, reader, sourceEnd, value, func(child box) error {
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
		return 0, nil, err
	}
	if !found {
		return 0, nil, fmt.Errorf("%w: dinf has no dref", errMalformedMovie)
	}
	return count, raw, nil
}

func parseDataReferences(ctx context.Context, reader access.Random, sourceEnd uint64, value box) (uint32, error) {
	var prefix [8]byte
	if err := readBoxPrefix(ctx, reader, value, prefix[:], "dref"); err != nil {
		return 0, err
	}
	if prefix[0] != 0 || prefix[1] != 0 || prefix[2] != 0 || prefix[3] != 0 {
		return 0, fmt.Errorf("%w: dref full-box header", errUnsupportedMovie)
	}
	want := binary.BigEndian.Uint32(prefix[4:8])
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
