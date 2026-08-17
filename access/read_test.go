package access

import (
	"context"
	"errors"
	"io"
	"math"
	"testing"
)

type readAtFunc func(context.Context, []byte, int64) (int, error)

func (f readAtFunc) ReadAt(ctx context.Context, destination []byte, offset int64) (int, error) {
	return f(ctx, destination, offset)
}

func TestReadFullAtFillsPartialReads(t *testing.T) {
	var calls int
	destination := make([]byte, 4)
	err := ReadFullAt(nil, readAtFunc(func(ctx context.Context, destination []byte, offset int64) (int, error) {
		if ctx == nil {
			t.Fatal("ReadFullAt passed a nil context")
		}
		wantOffset := int64(7 + calls*2)
		if offset != wantOffset {
			t.Fatalf("ReadAt call %d offset = %d, want %d", calls, offset, wantOffset)
		}
		count := copy(destination, []byte("abcd")[calls*2:calls*2+2])
		calls++
		return count, nil
	}), destination, 7)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("ReadAt calls = %d, want 2", calls)
	}
	if string(destination) != "abcd" {
		t.Fatalf("destination = %q, want %q", destination, "abcd")
	}
}

func TestReadFullAtHandlesEOF(t *testing.T) {
	tests := []struct {
		name string
		read readAtFunc
		want error
	}{
		{
			name: "empty",
			read: func(context.Context, []byte, int64) (int, error) { return 0, io.EOF },
			want: io.EOF,
		},
		{
			name: "partial",
			read: func(_ context.Context, destination []byte, _ int64) (int, error) {
				return copy(destination, []byte("ab")), io.EOF
			},
			want: io.ErrUnexpectedEOF,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ReadFullAt(context.Background(), test.read, make([]byte, 4), 0)
			if !errors.Is(err, test.want) {
				t.Fatalf("ReadFullAt error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestReadFullAtAcceptsCompleteReadWithEOF(t *testing.T) {
	err := ReadFullAt(context.Background(), readAtFunc(func(_ context.Context, destination []byte, _ int64) (int, error) {
		return copy(destination, []byte("abcd")), io.EOF
	}), make([]byte, 4), 0)
	if err != nil {
		t.Fatalf("ReadFullAt error = %v, want nil", err)
	}
}

func TestReadFullAtRejectsNoProgress(t *testing.T) {
	err := ReadFullAt(context.Background(), readAtFunc(func(context.Context, []byte, int64) (int, error) {
		return 0, nil
	}), make([]byte, 1), 0)
	if !errors.Is(err, io.ErrNoProgress) {
		t.Fatalf("ReadFullAt error = %v, want %v", err, io.ErrNoProgress)
	}
}

func TestReadFullAtRejectsInvalidArgumentsAndCounts(t *testing.T) {
	valid := readAtFunc(func(context.Context, []byte, int64) (int, error) { return 0, nil })
	tests := []struct {
		name   string
		reader Random
		data   []byte
		offset int64
	}{
		{name: "nil reader", reader: nil, data: make([]byte, 1)},
		{name: "negative offset", reader: valid, data: make([]byte, 1), offset: -1},
		{name: "negative count", reader: readAtFunc(func(context.Context, []byte, int64) (int, error) { return -1, nil }), data: make([]byte, 1)},
		{name: "count exceeds destination", reader: readAtFunc(func(context.Context, []byte, int64) (int, error) { return 2, nil }), data: make([]byte, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ReadFullAt(context.Background(), test.reader, test.data, test.offset); !errors.Is(err, ErrInvalidRead) {
				t.Fatalf("ReadFullAt error = %v, want ErrInvalidRead", err)
			}
		})
	}
}

func TestReadFullAtChecksContextBeforeEachRead(t *testing.T) {
	t.Run("initial", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		calls := 0
		err := ReadFullAt(ctx, readAtFunc(func(context.Context, []byte, int64) (int, error) {
			calls++
			return 1, nil
		}), make([]byte, 1), 0)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ReadFullAt error = %v, want context.Canceled", err)
		}
		if calls != 0 {
			t.Fatalf("ReadAt calls = %d, want 0", calls)
		}
	})

	t.Run("after partial read", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		calls := 0
		err := ReadFullAt(ctx, readAtFunc(func(_ context.Context, destination []byte, _ int64) (int, error) {
			calls++
			destination[0] = 1
			cancel()
			return 1, nil
		}), make([]byte, 2), 0)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ReadFullAt error = %v, want context.Canceled", err)
		}
		if calls != 1 {
			t.Fatalf("ReadAt calls = %d, want 1", calls)
		}
	})
}

func TestReadFullAtChecksOffsetOverflowBeforeNextRead(t *testing.T) {
	calls := 0
	err := ReadFullAt(context.Background(), readAtFunc(func(_ context.Context, destination []byte, offset int64) (int, error) {
		calls++
		if offset != math.MaxInt64 {
			t.Fatalf("first read offset = %d, want %d", offset, int64(math.MaxInt64))
		}
		destination[0] = 1
		return 1, nil
	}), make([]byte, 2), math.MaxInt64)
	if !errors.Is(err, ErrInvalidRead) {
		t.Fatalf("ReadFullAt error = %v, want ErrInvalidRead", err)
	}
	if calls != 1 {
		t.Fatalf("ReadAt calls = %d, want 1", calls)
	}
}

func TestReadFullAtAcceptsEmptyDestination(t *testing.T) {
	calls := 0
	err := ReadFullAt(context.Background(), readAtFunc(func(context.Context, []byte, int64) (int, error) {
		calls++
		return 0, nil
	}), nil, math.MaxInt64)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("ReadAt calls = %d, want 0", calls)
	}
}
