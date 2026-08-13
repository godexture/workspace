package file

import (
	"context"
	"errors"
	"io"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
)

type sourceOperator struct {
	shape      flow.Shape
	buffers    *buffer.Allocator
	sequential access.Sequential
	random     access.Random
	offset     int64
	done       bool
}

func openSource(shape flow.Shape, opening access.Opening, buffers *buffer.Allocator) (flow.Operator, error) {
	if !opening.Valid() || opening.Direction() != access.SourceDirection {
		return nil, errors.New("file source received an invalid Access opening")
	}
	if buffers == nil {
		return nil, errors.New("file source requires a payload buffer grant")
	}
	sequential, _ := access.SequentialOf(opening)
	random, _ := access.RandomOf(opening)
	if sequential == nil && random == nil {
		return nil, errors.New("file source received no selected read view")
	}
	return &sourceOperator{shape: shape.Clone(), buffers: buffers, sequential: sequential, random: random}, nil
}

func (o *sourceOperator) Ports() flow.Shape { return o.shape.Clone() }
func (*sourceOperator) Close() error        { return nil }

func (o *sourceOperator) Read(ctx context.Context, into *flow.Item[buffer.Handle]) error {
	if o.done {
		return io.EOF
	}
	lease, err := o.buffers.Overwrite(buffer.Spec{Alignment: 1, Planes: []buffer.PlaneSpec{{Size: blockSize}}})
	if err != nil {
		return err
	}
	defer lease.Discard()

	count := 0
	eof := false
	err = lease.Fill(func(storage buffer.Mutable) error {
		destination := storage.Bytes()
		for count < len(destination) {
			read, readErr := o.read(ctx, destination[count:])
			if read < 0 || read > len(destination)-count {
				return errors.New("file source returned an invalid read count")
			}
			count += read
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					eof = true
					return nil
				}
				return readErr
			}
			if read == 0 {
				return io.ErrNoProgress
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if count == 0 && eof {
		o.done = true
		return io.EOF
	}

	payload, err := lease.Commit()
	if err != nil {
		return err
	}
	if count != blockSize {
		// A zero-copy narrowing keeps the read inside the grant this component
		// declared; allocating a second exact buffer would need twice it.
		exact, rangeErr := payload.Range(0, count)
		payload.Release()
		if rangeErr != nil {
			return rangeErr
		}
		payload = exact
	}
	o.done = eof
	*into = flow.NewItem(payload, access.Bytes())
	return nil
}

func (o *sourceOperator) read(ctx context.Context, destination []byte) (int, error) {
	if o.sequential != nil {
		return o.sequential.Read(ctx, destination)
	}
	count, err := o.random.ReadAt(ctx, destination, o.offset)
	o.offset += int64(count)
	return count, err
}
