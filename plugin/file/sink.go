package file

import (
	"context"
	"errors"
	"io"
	"math"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
)

type sinkOperator struct {
	shape       flow.Shape
	appender    access.Appender
	patcher     access.Patcher
	transaction access.Transaction
	flusher     access.Flusher
	syncer      access.Syncer
	offset      int64
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

func (o *sinkOperator) Write(ctx context.Context, input flow.Input[access.Write]) error {
	if !input.Valid() || !input.Value().Valid() {
		return errors.New("file sink received an invalid write")
	}
	write := input.Value()
	var err error
	switch write.Operation() {
	case access.AppendOperation:
		err = o.append(ctx, write.Bytes())
	case access.PatchOperation:
		err = o.patch(ctx, write.Offset(), write.Bytes())
	default:
		err = errors.New("file sink received an unknown write operation")
	}
	if err != nil {
		return err
	}
	input.Drop()
	return nil
}

func (o *sinkOperator) append(ctx context.Context, payload []byte) error {
	if _, err := writeExtent(o.offset, len(payload)); err != nil {
		return err
	}
	if o.appender != nil {
		return writeAll(ctx, payload, func(ctx context.Context, remaining []byte, _ int64) (int, error) {
			return o.appender.Write(ctx, remaining)
		}, &o.offset)
	}
	if o.patcher == nil {
		return errors.New("file sink has no append-capable view")
	}
	return writeAll(ctx, payload, o.patcher.WriteAt, &o.offset)
}

func (o *sinkOperator) patch(ctx context.Context, offset int64, payload []byte) error {
	end, err := writeExtent(offset, len(payload))
	if err != nil {
		return err
	}
	if o.patcher == nil {
		return errors.New("file sink has no patch view")
	}
	position := offset
	if err := writeAll(ctx, payload, o.patcher.WriteAt, &position); err != nil {
		return err
	}
	if end > o.offset {
		o.offset = end
	}
	return nil
}

func writeAll(ctx context.Context, payload []byte, write func(context.Context, []byte, int64) (int, error), offset *int64) error {
	remaining := payload
	for len(remaining) != 0 {
		count, err := write(ctx, remaining, *offset)
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
