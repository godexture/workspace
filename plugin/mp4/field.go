package mp4

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/godexture/godec/access"
)

func readBoxPrefix(ctx context.Context, reader access.Random, value box, destination []byte, what string) error {
	if uint64(len(destination)) > value.payloadSize {
		return fmt.Errorf("%w: %s is shorter than %d bytes", errMalformedMovie, what, len(destination))
	}
	return readMovieAt(ctx, reader, destination, value.payloadOffset, what)
}

func readMovieAt(ctx context.Context, reader access.Random, destination []byte, offset uint64, what string) error {
	if offset > math.MaxInt64 || len(destination) > 0 && offset > uint64(math.MaxInt64)-uint64(len(destination)-1) {
		return fmt.Errorf("%w: %s read range exceeds runtime offsets", errMalformedMovie, what)
	}
	if err := access.ReadFullAt(ctx, reader, destination, int64(offset)); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return fmt.Errorf("%w: %s: %w", errTruncatedMovie, what, err)
		}
		return fmt.Errorf("%w: %s: %w", errMalformedMovie, what, err)
	}
	return nil
}

func payloadEnd(value box) (uint64, bool) {
	return checkedBoxAdd(value.payloadOffset, value.payloadSize)
}

func fullBox(data []byte, what string) (uint8, uint32, error) {
	if len(data) < 4 {
		return 0, 0, fmt.Errorf("%w: %s has no full-box header", errMalformedMovie, what)
	}
	return data[0], uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3]), nil
}
