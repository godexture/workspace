package mp4

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/resource"
)

func parseMovie(ctx context.Context, reader access.Random, sourceEnd uint64, readLimit, memoryLimit resource.Bytes) (movie, error) {
	if reader == nil || sourceEnd == 0 || readLimit == 0 || memoryLimit == 0 {
		return movie{}, fmt.Errorf("%w: parser input is invalid", errMalformedMovie)
	}
	inspection := newInspectReader(reader, readLimit)
	budget := newMovieBudget(memoryLimit)
	var result movie
	result.sourceEnd = sourceEnd
	var haveFileType, haveMovie, haveMedia bool
	err := scanTopLevelBoxes(ctx, inspection, sourceEnd, func(value box) error {
		switch value.typeID {
		case typeFTYP:
			if haveFileType {
				return fmt.Errorf("%w: ftyp is repeated", errMalformedMovie)
			}
			fileType, err := parseFileType(ctx, inspection, value)
			if err != nil {
				return err
			}
			result.fileType = fileType
			result.fileBox = value
			haveFileType = true
		case typeMOOV:
			if haveMovie {
				return fmt.Errorf("%w: moov is repeated", errMalformedMovie)
			}
			tracks, header, err := parseMoov(ctx, inspection, sourceEnd, value, &budget)
			if err != nil {
				return err
			}
			result.tracks = tracks
			result.moov = value
			result.header = header
			records, err := moovRecordsOffsets(ctx, inspection, sourceEnd, value)
			if err != nil {
				return err
			}
			result.offsetIndex = result.offsetIndex || records
			haveMovie = true
		case typeMDAT:
			if haveMedia {
				return fmt.Errorf("%w: multiple mdat boxes are not supported", errUnsupportedMovie)
			}
			result.media = value
			haveMedia = true
		case typeSIDX, typeSSIX, typeMFRA, typeTFRA, typeILOC:
			result.offsetIndex = true
		case typeMETA:
			records, err := metaRecordsOffsets(ctx, inspection, sourceEnd, value)
			if err != nil {
				return err
			}
			result.offsetIndex = result.offsetIndex || records
		case typeMOOF:
			return fmt.Errorf("%w: fragmented moof is not supported", errUnsupportedMovie)
		}
		return nil
	})
	if err != nil {
		return movie{}, normalizeMovieError(err)
	}
	if !haveFileType || !haveMovie || !haveMedia {
		return movie{}, fmt.Errorf("%w: ftyp, moov, and one mdat are required", errMalformedMovie)
	}
	for index := range result.tracks {
		total, err := validateTrack(ctx, inspection, result.media, &result.tracks[index])
		if err != nil {
			return movie{}, normalizeMovieError(err)
		}
		var ok bool
		result.tracks[index].sampleBytes = total
		result.totalSampleBytes, ok = checkedBoxAdd(result.totalSampleBytes, total)
		if !ok {
			return movie{}, fmt.Errorf("%w: movie sample payload total overflows", errMalformedMovie)
		}
	}
	return result, nil
}

// moovRecordsOffsets reports an iloc item index directly under moov. Deeper
// nesting is not searched: iloc lives at moov or file level, while the meta box
// under udta holds vocabulary metadata and follows the QuickTime layout in some
// writers, so scanning it would fail closed on ordinary files.
func moovRecordsOffsets(ctx context.Context, reader access.Random, sourceEnd uint64, value box) (bool, error) {
	found := false
	err := scanChildBoxes(ctx, reader, sourceEnd, value, func(child box) error {
		if child.typeID != typeMETA {
			return nil
		}
		records, err := metaRecordsOffsets(ctx, reader, sourceEnd, child)
		if err != nil {
			return err
		}
		found = found || records
		return nil
	})
	return found, err
}

// metaRecordsOffsets reports whether a meta box may carry an iloc item index.
// meta is a FullBox in ISO BMFF but not in the QuickTime variant, so a child
// list this reader cannot walk counts as an index and fails closed.
func metaRecordsOffsets(ctx context.Context, reader access.Random, sourceEnd uint64, value box) (bool, error) {
	start, ok := checkedBoxAdd(value.payloadOffset, 4)
	end, endOK := payloadEnd(value)
	if !ok || !endOK || start > end {
		return true, nil
	}
	found := false
	err := scanBoxes(ctx, reader, boxScope{sourceEnd: sourceEnd, start: start, end: end}, func(child box) error {
		if child.typeID == typeILOC {
			found = true
		}
		return nil
	})
	if err != nil && unwalkableBox(err) {
		return true, nil
	}
	return found, err
}

// unwalkableBox separates a box structure this reader cannot follow from a
// budget, cancellation or I/O failure. Only the former classifies content; the
// latter says nothing about it and must reach the caller.
func unwalkableBox(err error) bool {
	return errors.Is(err, errMalformedBox) && !errors.Is(err, errUnsupportedMovie) && !errors.Is(err, errTruncatedMovie)
}

func readMovieHeader(ctx context.Context, reader access.Random, value box) (movieHeader, error) {
	var prefix [112]byte
	if err := readBoxPrefix(ctx, reader, value, prefix[:4], "mvhd"); err != nil {
		return movieHeader{}, err
	}
	length := 100
	if prefix[0] == 1 {
		length = 112
	}
	if err := readBoxPrefix(ctx, reader, value, prefix[:length], "mvhd"); err != nil {
		return movieHeader{}, err
	}
	return parseMovieHeader(prefix[:length], value)
}

func parseMovieHeader(data []byte, value box) (movieHeader, error) {
	if len(data) < 4 {
		return movieHeader{}, fmt.Errorf("%w: mvhd has no full-box header", errMalformedMovie)
	}
	result := movieHeader{box: value}
	var durationOffset uint64
	switch data[0] {
	case 0:
		if len(data) < 100 {
			return movieHeader{}, fmt.Errorf("%w: mvhd version zero is shorter than 100 bytes", errMalformedMovie)
		}
		result.timeScale = binary.BigEndian.Uint32(data[12:16])
		result.duration.value = uint64(binary.BigEndian.Uint32(data[16:20]))
		durationOffset = 16
	case 1:
		if len(data) < 112 {
			return movieHeader{}, fmt.Errorf("%w: mvhd version one is shorter than 112 bytes", errMalformedMovie)
		}
		result.timeScale = binary.BigEndian.Uint32(data[20:24])
		result.duration.value = binary.BigEndian.Uint64(data[24:32])
		result.duration.wide = true
		durationOffset = 24
	default:
		return movieHeader{}, fmt.Errorf("%w: mvhd version %d", errUnsupportedMovie, data[0])
	}
	if result.timeScale == 0 {
		return movieHeader{}, fmt.Errorf("%w: mvhd timescale is zero", errMalformedMovie)
	}
	location, ok := checkedBoxAdd(value.payloadOffset, durationOffset)
	if !ok {
		return movieHeader{}, fmt.Errorf("%w: mvhd duration offset overflows", errMalformedMovie)
	}
	result.duration.offset = location
	return result, nil
}

func parseFileType(ctx context.Context, reader access.Random, value box) (fileType, error) {
	if value.payloadSize < 8 || (value.payloadSize-8)%4 != 0 {
		return fileType{}, fmt.Errorf("%w: ftyp has an invalid brand list", errMalformedMovie)
	}
	var prefix [8]byte
	if err := readBoxPrefix(ctx, reader, value, prefix[:], "ftyp"); err != nil {
		return fileType{}, err
	}
	result := fileType{major: boxType(prefix[:4]), minor: binary.BigEndian.Uint32(prefix[4:])}
	supported := knownMP4Brand(result.major)
	for offset := uint64(8); offset < value.payloadSize; offset += 4 {
		var compatible [4]byte
		if err := readMovieAt(ctx, reader, compatible[:], value.payloadOffset+offset, "ftyp compatible brand"); err != nil {
			return fileType{}, err
		}
		supported = supported || knownMP4Brand(boxType(compatible[:]))
	}
	if !supported {
		return fileType{}, fmt.Errorf("%w: ftyp major brand %q is not an MP4 brand", errUnsupportedMovie, result.major)
	}
	return result, nil
}

func normalizeMovieError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, errMalformedMovie) || errors.Is(err, errUnsupportedMovie) || errors.Is(err, errTruncatedMovie) {
		return err
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: %w", errTruncatedMovie, err)
	}
	if errors.Is(err, errMalformedBox) {
		return fmt.Errorf("%w: %w", errMalformedMovie, err)
	}
	// Anything else came from the source rather than from the bytes it holds.
	return err
}
