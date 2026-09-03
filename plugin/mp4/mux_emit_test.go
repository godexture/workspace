package mp4

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/metadata"
)

type cancelAfterMuxWriteCollector struct {
	muxWriteCollector
	cancel context.CancelFunc
}

func (c *cancelAfterMuxWriteCollector) Emit(ctx context.Context, input *flow.Item[access.Write]) error {
	if err := c.muxWriteCollector.Emit(ctx, input); err != nil {
		return err
	}
	if len(c.items) == 1 {
		c.cancel()
	}
	return nil
}

func TestMP4MuxEmitBlobChecksCancellationBetweenPages(t *testing.T) {
	value := metadata.NewBlob(ilstMediaType, bytes.Repeat([]byte{0x7a}, muxPageBytes+1))
	mux := &muxer{buffers: mustMP4Allocator(t, 2*muxPageBytes)}
	ctx, cancel := context.WithCancel(t.Context())
	collector := &cancelAfterMuxWriteCollector{cancel: cancel}
	err := mux.emitBlob(ctx, value, collector)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled blob emission = %v, want context.Canceled", err)
	}
	if len(collector.items) != 1 {
		t.Fatalf("cancelled blob emission writes = %d, want one page", len(collector.items))
	}
	for _, item := range collector.items {
		item.Drop()
	}
}
