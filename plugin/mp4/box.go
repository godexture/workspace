package mp4

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/godexture/godec/access"
)

var errMalformedBox = errors.New("malformed ISO BMFF box")

type boxType [4]byte

type box struct {
	typeID        boxType
	offset        uint64
	size          uint64
	headerSize    uint64
	payloadOffset uint64
	payloadSize   uint64
	userType      [16]byte
	hasUserType   bool
}

type boxScope struct {
	sourceEnd uint64
	start     uint64
	end       uint64
	topLevel  bool
}

type boxVisitor func(box) error

func scanTopLevelBoxes(ctx context.Context, reader access.Random, sourceEnd uint64, visit boxVisitor) error {
	return scanBoxes(ctx, reader, boxScope{sourceEnd: sourceEnd, end: sourceEnd, topLevel: true}, visit)
}

func scanChildBoxes(ctx context.Context, reader access.Random, sourceEnd uint64, parent box, visit boxVisitor) error {
	end, ok := checkedBoxAdd(parent.payloadOffset, parent.payloadSize)
	if !ok {
		return fmt.Errorf("%w: parent payload range overflows", errMalformedBox)
	}
	return scanBoxes(ctx, reader, boxScope{
		sourceEnd: sourceEnd,
		start:     parent.payloadOffset,
		end:       end,
	}, visit)
}

func scanBoxes(ctx context.Context, reader access.Random, scope boxScope, visit boxVisitor) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := context.Cause(ctx); err != nil {
		return err
	}
	if reader == nil || visit == nil || scope.start > scope.end || scope.end > scope.sourceEnd {
		return fmt.Errorf("%w: invalid box scan", errMalformedBox)
	}

	for offset := scope.start; offset < scope.end; {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		value, err := readBox(ctx, reader, scope, offset)
		if err != nil {
			return err
		}
		if err := visit(value); err != nil {
			return err
		}
		next, ok := checkedBoxAdd(offset, value.size)
		if !ok || next <= offset || next > scope.end {
			return fmt.Errorf("%w: box at %d exceeds its containing range", errMalformedBox, offset)
		}
		offset = next
	}
	return nil
}

func readBox(ctx context.Context, reader access.Random, scope boxScope, offset uint64) (box, error) {
	if scope.end-offset < 8 {
		return box{}, fmt.Errorf("%w: truncated box header at %d", errMalformedBox, offset)
	}
	if offset > math.MaxInt64 {
		return box{}, fmt.Errorf("%w: box offset %d exceeds runtime range", errMalformedBox, offset)
	}

	var fixed [8]byte
	if err := access.ReadFullAt(ctx, reader, fixed[:], int64(offset)); err != nil {
		return box{}, fmt.Errorf("%w: box header at %d: %w", errMalformedBox, offset, err)
	}

	size32 := binary.BigEndian.Uint32(fixed[:4])
	extendsToEOF := size32 == 0
	declaredSize := uint64(size32)
	value := box{offset: offset, typeID: boxType(fixed[4:8])}
	headerSize := uint64(8)
	if declaredSize == 1 {
		if scope.end-offset < headerSize+8 {
			return box{}, fmt.Errorf("%w: truncated large-size header at %d", errMalformedBox, offset)
		}
		largeOffset, ok := checkedBoxAdd(offset, headerSize)
		if !ok || largeOffset > math.MaxInt64 {
			return box{}, fmt.Errorf("%w: large-size offset exceeds runtime range", errMalformedBox)
		}
		var large [8]byte
		if err := access.ReadFullAt(ctx, reader, large[:], int64(largeOffset)); err != nil {
			return box{}, fmt.Errorf("%w: large size at %d: %w", errMalformedBox, offset, err)
		}
		declaredSize = binary.BigEndian.Uint64(large[:])
		headerSize += 8
	}
	if value.typeID == (boxType{'u', 'u', 'i', 'd'}) {
		if scope.end-offset < headerSize+16 {
			return box{}, fmt.Errorf("%w: truncated uuid header at %d", errMalformedBox, offset)
		}
		userTypeOffset, ok := checkedBoxAdd(offset, headerSize)
		if !ok || userTypeOffset > math.MaxInt64 {
			return box{}, fmt.Errorf("%w: uuid offset exceeds runtime range", errMalformedBox)
		}
		if err := access.ReadFullAt(ctx, reader, value.userType[:], int64(userTypeOffset)); err != nil {
			return box{}, fmt.Errorf("%w: uuid user type at %d: %w", errMalformedBox, offset, err)
		}
		value.hasUserType = true
		headerSize += 16
	}

	if extendsToEOF {
		if !scope.topLevel {
			return box{}, fmt.Errorf("%w: nested box at %d has size zero", errMalformedBox, offset)
		}
		declaredSize = scope.sourceEnd - offset
	}
	if declaredSize < headerSize {
		return box{}, fmt.Errorf("%w: box at %d is smaller than its header", errMalformedBox, offset)
	}
	end, ok := checkedBoxAdd(offset, declaredSize)
	if !ok || end > scope.end || end > scope.sourceEnd {
		return box{}, fmt.Errorf("%w: box at %d exceeds its containing range", errMalformedBox, offset)
	}
	payloadOffset, ok := checkedBoxAdd(offset, headerSize)
	if !ok {
		return box{}, fmt.Errorf("%w: payload offset overflows", errMalformedBox)
	}

	value.size = declaredSize
	value.headerSize = headerSize
	value.payloadOffset = payloadOffset
	value.payloadSize = declaredSize - headerSize
	return value, nil
}

func checkedBoxAdd(left, right uint64) (uint64, bool) {
	if left > math.MaxUint64-right {
		return 0, false
	}
	return left + right, true
}
