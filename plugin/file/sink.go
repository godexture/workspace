package file

import (
	"context"
	"errors"
	"io"

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

func (o *sinkOperator) Write(ctx context.Context, input flow.Input[buffer.Handle]) error {
	if !input.Valid() || !input.Value().Valid() {
		return errors.New("file sink received invalid bytes")
	}
	remaining := input.Value().Bytes()
	for len(remaining) != 0 {
		var count int
		var err error
		if o.appender != nil {
			count, err = o.appender.Write(ctx, remaining)
		} else {
			count, err = o.patcher.WriteAt(ctx, remaining, o.offset)
		}
		if count < 0 || count > len(remaining) {
			return errors.New("file sink returned an invalid write count")
		}
		o.offset += int64(count)
		remaining = remaining[count:]
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrShortWrite
		}
	}
	input.Drop()
	return nil
}

func (o *sinkOperator) Flush(ctx context.Context) error { return o.flusher.Flush(ctx) }
func (o *sinkOperator) Sync(ctx context.Context) error  { return o.syncer.Sync(ctx) }
func (o *sinkOperator) PrepareCommit(ctx context.Context) error {
	return o.transaction.PrepareCommit(ctx)
}
func (o *sinkOperator) Commit(ctx context.Context) error { return o.transaction.Commit(ctx) }
func (o *sinkOperator) Abort(ctx context.Context) error  { return o.transaction.Abort(ctx) }
