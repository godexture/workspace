package pipeline

import (
	"context"

	"github.com/godexture/core/node"
	"golang.org/x/sync/errgroup"
)

type Runner struct {
	resolver ResolverBundle
}

func NewPipeline(ctx context.Context, resolver ResolverBundle) *Runner {
	return &Runner{
		resolver: resolver,
	}
}

func NewRunner() *Runner {
	return &Runner{}
}

func (r *Runner) Run(ctx context.Context, nodes []node.Node) error {
	eg, egCtx := errgroup.WithContext(ctx)

	for _, n := range nodes {
		n := n

		eg.Go(func() error {
			return n.Start(egCtx)
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	return nil
}
