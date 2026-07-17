package engine

import (
	"context"
	"io"

	"github.com/godexture/core/node"
)

type outputNotifier interface {
	OutputReady() <-chan struct{}
}

func runCodecLoop[I any, O any](
	ctx context.Context,
	in node.Edge[I],
	out node.Edge[O],
	send func(I) error,
	receive func() (O, error),
	flush func() error,
) error {
	defer out.Close()

	for {
		input, err := in.Pull(ctx)
		if err == io.EOF {
			if err := flush(); err != nil {
				return err
			}
			for {
				output, err := receive()
				if err == ErrEAGAIN || err == io.EOF || err == ErrEOF {
					return nil
				} else if err != nil {
					return err
				}
				if err := out.Push(ctx, output); err != nil {
					return err
				}
			}
		} else if err != nil {
			return err
		}

		if err := send(input); err != nil {
			return err
		}

		for {
			output, err := receive()
			if err == ErrEAGAIN {
				break
			} else if err != nil {
				return err
			}
			if err := out.Push(ctx, output); err != nil {
				return err
			}
		}
	}
}

type inputResult[T any] struct {
	value T
	err   error
}

func runAsyncCodecLoop[I any, O any](
	ctx context.Context,
	in node.Edge[I],
	out node.Edge[O],
	send func(I) error,
	receive func() (O, error),
	flush func() error,
	notifier outputNotifier,
) error {
	defer out.Close()

	inputs := make(chan inputResult[I], 1)
	go func() {
		defer close(inputs)
		for {
			value, err := in.Pull(ctx)
			select {
			case inputs <- inputResult[I]{value: value, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	inputOpen := true
	for {
		ready := notifier.OutputReady()
		if !inputOpen && ready == nil {
			return nil
		}

		select {
		case input, ok := <-inputs:
			if !ok {
				inputs = nil
				inputOpen = false
				continue
			}
			if input.err == io.EOF {
				inputs = nil
				inputOpen = false
				if err := flush(); err != nil {
					return err
				}
				continue
			}
			if input.err != nil {
				return input.err
			}
			if err := send(input.value); err != nil {
				return err
			}

		case <-ready:
			output, err := receive()
			if err == ErrEAGAIN {
				continue
			}
			if err == io.EOF || err == ErrEOF {
				return nil
			}
			if err != nil {
				return err
			}
			if err := out.Push(ctx, output); err != nil {
				return err
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
