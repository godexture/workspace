package access

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
)

// ErrInvalidRead reports an invalid ReadFullAt argument or an offset/count
// combination that cannot be represented by Random.
var ErrInvalidRead = errors.New("access random read is invalid")

// ReadFullAt fills destination from reader starting at offset. It keeps the
// absolute offset contract of Random while tolerating short successful reads.
func ReadFullAt(ctx context.Context, reader Random, destination []byte, offset int64) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if reader == nil || offset < 0 {
		return ErrInvalidRead
	}
	if len(destination) == 0 {
		return nil
	}
	if offset > math.MaxInt64-int64(len(destination)-1) {
		return fmt.Errorf("%w: read range overflows", ErrInvalidRead)
	}

	read := 0
	for read < len(destination) {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		if offset > math.MaxInt64-int64(read) {
			return fmt.Errorf("%w: read offset overflows", ErrInvalidRead)
		}
		count, err := reader.ReadAt(ctx, destination[read:], offset+int64(read))
		if count < 0 || count > len(destination)-read {
			return fmt.Errorf("%w: invalid random read count", ErrInvalidRead)
		}
		read += count
		if read == len(destination) {
			return nil
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if read == 0 {
					return io.EOF
				}
				return io.ErrUnexpectedEOF
			}
			return err
		}
		if count == 0 {
			return io.ErrNoProgress
		}
	}
	return nil
}
