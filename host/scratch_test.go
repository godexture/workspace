package host

import (
	"context"
	"errors"
	"testing"

	"github.com/godexture/godec/job"
	"github.com/godexture/godec/resource"
)

type scratchLeaseFixture struct {
	close func() error
}

func (scratchLeaseFixture) Append(context.Context, []byte) (int64, error) { return 0, nil }
func (scratchLeaseFixture) ReadAt(context.Context, []byte, int64) error   { return nil }
func (scratchLeaseFixture) WriteAt(context.Context, []byte, int64) error  { return nil }
func (f scratchLeaseFixture) Close() error                                { return f.close() }

func TestOpenScratchClaimsJoinsEveryRollbackFailure(t *testing.T) {
	openFailure := errors.New("open third scratch")
	closeFirst := errors.New("close first scratch")
	closeSecond := errors.New("close second scratch")
	var calls []string
	claims := []scratchClaim{
		{node: job.NodeID("first"), maximum: 1},
		{node: job.NodeID("second"), maximum: 1},
		{node: job.NodeID("third"), maximum: 1},
	}
	_, err := openScratchClaims(claims, func(resource.Bytes) (scratchLease, error) {
		switch len(calls) {
		case 0:
			calls = append(calls, "open/first")
			return scratchLeaseFixture{close: func() error {
				calls = append(calls, "close/first")
				return closeFirst
			}}, nil
		case 1:
			calls = append(calls, "open/second")
			return scratchLeaseFixture{close: func() error {
				calls = append(calls, "close/second")
				return closeSecond
			}}, nil
		default:
			calls = append(calls, "open/third")
			return nil, openFailure
		}
	})
	if !errors.Is(err, openFailure) || !errors.Is(err, closeFirst) || !errors.Is(err, closeSecond) {
		t.Fatalf("rollback errors = %v", err)
	}
	want := []string{"open/first", "open/second", "open/third", "close/second", "close/first"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v", calls)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}
