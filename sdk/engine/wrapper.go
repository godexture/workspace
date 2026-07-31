package engine

import (
	"context"
	"io"
	"sync"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
)

type outputNotifier interface {
	OutputReady() <-chan struct{}
}

// engineCloser is implemented by engines that own resources. Adapters expose
// it through the mandatory node lifecycle and make repeated Close calls safe.
type engineCloser interface {
	Close() error
}

type engineLifecycle struct {
	closer   engineCloser
	preparer registry.Preparer
	once     sync.Once
	err      error
}

func newEngineLifecycle(engine any) engineLifecycle {
	closer, _ := engine.(engineCloser)
	preparer, _ := engine.(registry.Preparer)
	return engineLifecycle{closer: closer, preparer: preparer}
}

func (l *engineLifecycle) Prepare(resources registry.ResourceGrant) error {
	if l.preparer == nil {
		return nil
	}
	return l.preparer.Prepare(resources)
}

func (l *engineLifecycle) Close() error {
	l.once.Do(func() {
		if l.closer != nil {
			l.err = l.closer.Close()
		}
	})
	return l.err
}

func runCodecLoop[I media.Retainer, O media.Retainer](
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
					output.Release()
					return err
				}
			}
		} else if err != nil {
			return err
		}

		if err := send(input); err != nil {
			input.Release()
			return err
		}

		for {
			output, err := receive()
			if err == ErrEAGAIN {
				input.Release()
				break
			} else if err != nil {
				input.Release()
				return err
			}
			if err := out.Push(ctx, output); err != nil {
				output.Release()
				input.Release()
				return err
			}
		}
	}
}

type inputResult[T any] struct {
	value T
	err   error
}

func runAsyncCodecLoop[I media.Retainer, O media.Retainer](
	ctx context.Context,
	in node.Edge[I],
	out node.Edge[O],
	send func(I) error,
	receive func() (O, error),
	flush func() error,
	notifier outputNotifier,
) error {
	defer out.Close()

	pullCtx, cancelPull := context.WithCancel(ctx)
	inputResults := make(chan inputResult[I], 1)
	inputs := inputResults
	go func() {
		defer close(inputResults)
		for {
			value, err := in.Pull(pullCtx)
			select {
			case inputResults <- inputResult[I]{value: value, err: err}:
			case <-pullCtx.Done():
				if err == nil {
					value.Release()
				}
				return
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() {
		cancelPull()
		for input := range inputResults {
			if input.err == nil {
				input.value.Release()
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
			err := send(input.value)
			input.value.Release()
			if err != nil {
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
				output.Release()
				return err
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
