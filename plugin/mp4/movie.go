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
	if reader == nil || readLimit == 0 || memoryLimit == 0 {
		return movie{}, fmt.Errorf("%w: parser input is invalid", errMalformedMovie)
	}
	budget := newMovieBudget(readLimit, memoryLimit)
	var result movie
	var haveFileType, haveMovie bool
	err := scanTopLevelBoxes(ctx, reader, sourceEnd, func(value box) error {
		switch value.typeID {
		case typeFTYP:
			if haveFileType {
				return fmt.Errorf("%w: ftyp is repeated", errMalformedMovie)
			}
			fileType, err := parseFileType(ctx, reader, value, &budget)
			if err != nil {
				return err
			}
			if err := budget.reserve(anchorBudgetBytes, "top-level anchor"); err != nil {
				return err
			}
			result.fileType = fileType
			haveFileType = true
			result.top = append(result.top, anchor{typeID: value.typeID, index: 0})
		case typeMOOV:
			if haveMovie {
				return fmt.Errorf("%w: moov is repeated", errMalformedMovie)
			}
			tracks, anchors, err := parseMoov(ctx, reader, sourceEnd, value, &budget)
			if err != nil {
				return err
			}
			if err := budget.reserve(anchorBudgetBytes, "top-level anchor"); err != nil {
				return err
			}
			result.tracks = tracks
			result.moov = anchors
			haveMovie = true
			result.top = append(result.top, anchor{typeID: value.typeID, index: 0})
		case typeMDAT:
			end, ok := payloadEnd(value)
			if !ok {
				return fmt.Errorf("%w: mdat payload overflows", errMalformedMovie)
			}
			if err := budget.reserve(anchorBudgetBytes, "mdat anchor"); err != nil {
				return err
			}
			result.media = append(result.media, mediaRange{start: value.payloadOffset, end: end})
			result.top = append(result.top, anchor{typeID: value.typeID, index: len(result.media) - 1})
		case typeMOOF:
			return fmt.Errorf("%w: fragmented moof is not supported", errUnsupportedMovie)
		default:
			raw, err := preserveRaw(ctx, reader, value, &budget, "top-level box")
			if err != nil {
				return err
			}
			result.top = append(result.top, anchor{typeID: value.typeID, index: -1, raw: raw})
		}
		return nil
	})
	if err != nil {
		return movie{}, normalizeMovieError(err)
	}
	if !haveFileType || !haveMovie || len(result.media) == 0 {
		return movie{}, fmt.Errorf("%w: ftyp, moov, and one mdat are required", errMalformedMovie)
	}
	for index := range result.tracks {
		if err := expandTrack(&result.tracks[index], result.media, &budget); err != nil {
			return movie{}, err
		}
	}
	return result, nil
}

func parseMovieHeader(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("%w: mvhd has no full-box header", errMalformedMovie)
	}
	var timeScale uint32
	switch data[0] {
	case 0:
		if len(data) < 100 {
			return fmt.Errorf("%w: mvhd version zero is shorter than 100 bytes", errMalformedMovie)
		}
		timeScale = binary.BigEndian.Uint32(data[12:16])
	case 1:
		if len(data) < 112 {
			return fmt.Errorf("%w: mvhd version one is shorter than 112 bytes", errMalformedMovie)
		}
		timeScale = binary.BigEndian.Uint32(data[20:24])
	default:
		return fmt.Errorf("%w: mvhd version %d", errUnsupportedMovie, data[0])
	}
	if timeScale == 0 {
		return fmt.Errorf("%w: mvhd timescale is zero", errMalformedMovie)
	}
	return nil
}

func parseFileType(ctx context.Context, reader access.Random, value box, budget *movieBudget) (fileType, error) {
	data, err := readBoxData(ctx, reader, value, budget, "ftyp")
	if err != nil {
		return fileType{}, err
	}
	if len(data) < 8 || (len(data)-8)%4 != 0 {
		return fileType{}, fmt.Errorf("%w: ftyp has an invalid brand list", errMalformedMovie)
	}
	result := fileType{
		major: boxType(data[:4]),
		minor: binary.BigEndian.Uint32(data[4:8]),
	}
	if err := budget.reserveRecords(uint64((len(data)-8)/4), 8, "compatible brands"); err != nil {
		return fileType{}, err
	}
	supported := knownMP4Brand(result.major)
	for offset := 8; offset < len(data); offset += 4 {
		compatible := boxType(data[offset : offset+4])
		result.compatible = append(result.compatible, compatible)
		supported = supported || knownMP4Brand(compatible)
	}
	if !supported {
		return fileType{}, fmt.Errorf("%w: ftyp major brand %q is not an MP4 brand", errUnsupportedMovie, result.major)
	}
	return result, nil
}

func preserveRaw(ctx context.Context, reader access.Random, value box, budget *movieBudget, what string) ([]byte, error) {
	if err := budget.reserve(anchorBudgetBytes, what+" anchor"); err != nil {
		return nil, err
	}
	return readRawBox(ctx, reader, value, budget, what)
}

func normalizeMovieError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, errMalformedMovie) || errors.Is(err, errUnsupportedMovie) || errors.Is(err, errTruncatedMovie) {
		return err
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: %w", errTruncatedMovie, err)
	}
	return fmt.Errorf("%w: %w", errMalformedMovie, err)
}
