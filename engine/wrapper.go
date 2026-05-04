package engine

import (
	"context"
	"io"

	"github.com/godexture/core/node"
)

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
