package file

import (
	"context"
	"errors"
	"io"
	"math"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
)

type sinkOperator struct {
	shape       flow.Shape
	appender    access.Appender
	patcher     access.Patcher
	transaction access.Transaction
	flusher     access.Flusher
	syncer      access.Syncer
	offset      int64
	scratch     [blockSize]byte
}

type appendWriter struct{ appender access.Appender }

func (w appendWriter) WriteAt(ctx context.Context, source []byte, _ int64) (int, error) {
	return w.appender.Write(ctx, source)
}

func openSink(shape flow.Shape, opening access.Opening) (flow.Operator, error) {
	if !opening.Valid() || opening.Direction() != access.SinkDirection {
		return nil, errors.New("file sink received an invalid Access opening")
	}
	appender, _ := access.AppenderOf(opening)
	patcher, _ := access.PatcherOf(opening)
	transaction, transactionOK := access.TransactionOf(opening)
	flusher, flusherOK := access.FlusherOf(opening)
	syncer, syncerOK := access.SyncerOf(opening)
	if appender == nil && patcher == nil {
		return nil, errors.New("file sink received no selected write view")
	}
	if !transactionOK || !flusherOK || !syncerOK {
		return nil, errors.New("file sink session is missing its commit services")
	}
	return &sinkOperator{
		shape:       shape.Clone(),
		appender:    appender,
		patcher:     patcher,
		transaction: transaction,
		flusher:     flusher,
		syncer:      syncer,
	}, nil
}

func (o *sinkOperator) Ports() flow.Shape { return o.shape.Clone() }
func (*sinkOperator) Close() error        { return nil }

func (o *sinkOperator) Write(ctx context.Context, input *flow.Item[access.Write]) error {
	defer input.Drop()
	if !input.Valid() || !input.Value().Valid() {
		return errors.New("file sink received an invalid write")
	}
	write := input.Value()
	payload := write.Bytes()
	switch write.Operation() {
	case access.AppendOperation:
		return o.append(ctx, payload)
	case access.PatchOperation:
		return o.patch(ctx, write.Offset(), payload)
	default:
		return errors.New("file sink received an unknown write operation")
	}
}

func (o *sinkOperator) append(ctx context.Context, payload buffer.Bytes) error {
	if _, err := writeExtent(o.offset, payload.Len()); err != nil {
		return err
	}
	if o.appender != nil {
		return o.writeView(ctx, payload, appendWriter{appender: o.appender}, &o.offset)
	}
	if o.patcher == nil {
		return errors.New("file sink has no append-capable view")
	}
	return o.writeView(ctx, payload, o.patcher, &o.offset)
}

func (o *sinkOperator) patch(ctx context.Context, offset int64, payload buffer.Bytes) error {
	end, err := writeExtent(offset, payload.Len())
	if err != nil {
		return err
	}
	if o.patcher == nil {
		return errors.New("file sink has no patch view")
	}
	position := offset
	if err := o.writeView(ctx, payload, o.patcher, &position); err != nil {
		return err
	}
	if end > o.offset {
		o.offset = end
	}
	return nil
}

func (o *sinkOperator) writeView(ctx context.Context, payload buffer.Bytes, writer access.Patcher, offset *int64) error {
	if payload.Len() == 0 {
		return nil
	}
	return payload.Blocks(o.scratch[:], func(block []byte, _ int) error {
		return writeAll(ctx, block, writer, offset)
	})
}

func writeAll(ctx context.Context, payload []byte, writer access.Patcher, offset *int64) error {
	remaining := payload
	for len(remaining) != 0 {
		count, err := writer.WriteAt(ctx, remaining, *offset)
		if count < 0 || count > len(remaining) {
			return errors.New("file sink returned an invalid write count")
		}
		*offset += int64(count)
		remaining = remaining[count:]
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func writeExtent(offset int64, size int) (int64, error) {
	if offset < 0 || uint64(size) > uint64(math.MaxInt64-offset) {
		return 0, errors.New("file sink write extent overflows int64")
	}
	return offset + int64(size), nil
}

func (o *sinkOperator) Flush(ctx context.Context) error { return o.flusher.Flush(ctx) }
func (o *sinkOperator) Sync(ctx context.Context) error  { return o.syncer.Sync(ctx) }
func (o *sinkOperator) PrepareCommit(ctx context.Context) error {
	return o.transaction.PrepareCommit(ctx)
}
func (o *sinkOperator) Commit(ctx context.Context) error { return o.transaction.Commit(ctx) }
func (o *sinkOperator) Abort(ctx context.Context) error  { return o.transaction.Abort(ctx) }
