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
			tracks, movieHead, err := parseMoov(ctx, inspection, sourceEnd, value, &budget)
			if err != nil {
				return err
			}
			result.tracks = tracks
			result.moov = value
			result.movieHead = movieHead
			haveMovie = true
		case typeMDAT:
			if haveMedia {
				return fmt.Errorf("%w: multiple mdat boxes are not supported", errUnsupportedMovie)
			}
			result.media = value
			haveMedia = true
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
		result.totalSampleBytes, ok = checkedBoxAdd(result.totalSampleBytes, total)
		if !ok {
			return movie{}, fmt.Errorf("%w: movie sample payload total overflows", errMalformedMovie)
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

func validateMovieHeader(ctx context.Context, reader access.Random, value box) error {
	var prefix [112]byte
	if err := readBoxPrefix(ctx, reader, value, prefix[:4], "mvhd"); err != nil {
		return err
	}
	length := 100
	if prefix[0] == 1 {
		length = 112
	}
	if err := readBoxPrefix(ctx, reader, value, prefix[:length], "mvhd"); err != nil {
		return err
	}
	return parseMovieHeader(prefix[:length])
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
	return fmt.Errorf("%w: %w", errMalformedMovie, err)
}
