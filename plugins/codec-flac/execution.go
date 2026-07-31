package flac

import (
	"fmt"
	"runtime"

	"github.com/godexture/godec/plugins/codec-flac/internal/decoder"
	"github.com/godexture/godec/plugins/codec-flac/internal/encoder"
	"github.com/godexture/godec/core/registry"
)

type EngineOption interface {
	applyEngineOption(*engineOptions)
}

type engineOptions struct {
	parallelism int
}

type parallelismOption int

func (o parallelismOption) applyEngineOption(options *engineOptions) {
	options.parallelism = int(o)
}

// WithParallelism sets the execution parallelism without changing codec
// semantics or encoded output.
func WithParallelism(parallelism int) EngineOption {
	return parallelismOption(parallelism)
}

func resolveEngineOptions(options []EngineOption) (engineOptions, error) {
	resolved := engineOptions{parallelism: runtime.GOMAXPROCS(0)}
	for _, option := range options {
		if option == nil {
			return engineOptions{}, fmt.Errorf("FLAC engine option must not be nil")
		}
		option.applyEngineOption(&resolved)
	}
	if resolved.parallelism < 1 {
		return engineOptions{}, fmt.Errorf("FLAC parallelism must be positive: %d", resolved.parallelism)
	}
	return resolved, nil
}

// newOwnedPool builds a pool for standalone (non-pipeline) engine
// construction, where there is no negotiator to share one across stages.
// nil (parallelism <= 1) means the sequential path.
func (o engineOptions) newOwnedPool() *registry.WorkerPool {
	if o.parallelism <= 1 {
		return nil
	}
	return registry.NewWorkerPool(o.parallelism)
}

// ownedPoolEncoderEngine closes its privately-created pool on both Flush and
// Close, since nothing else shares it. Neither hook alone is reliable:
// engine.EncoderEngine has no Close, so a caller driving it directly (no
// pipeline) only ever calls Flush; a pipeline node wraps this via
// engine.WrapEncoder, whose adapter calls Close exactly once during teardown
// regardless of how the stream ends, but Flush there only runs on a clean
// end-of-stream and never if the node is instead cancelled mid-stream (e.g. a
// sibling branch errors first). WorkerPool.Close is idempotent, so covering
// both call sites is safe even when both fire for the same run.
type ownedPoolEncoderEngine struct {
	*encoder.Encoder
	pool *registry.WorkerPool
}

func (e *ownedPoolEncoderEngine) Flush() error {
	err := e.Encoder.Flush()
	if closeErr := e.pool.Close(); err == nil {
		err = closeErr
	}
	return err
}

func (e *ownedPoolEncoderEngine) Close() error {
	err := e.Encoder.Close()
	if closeErr := e.pool.Close(); err == nil {
		err = closeErr
	}
	return err
}

// ownedPoolDecoderEngine is the decoder counterpart of
// ownedPoolEncoderEngine.
type ownedPoolDecoderEngine struct {
	*decoder.Decoder
	pool *registry.WorkerPool
}

func (d *ownedPoolDecoderEngine) Flush() error {
	err := d.Decoder.Flush()
	if closeErr := d.pool.Close(); err == nil {
		err = closeErr
	}
	return err
}

func (d *ownedPoolDecoderEngine) Close() error {
	err := d.Decoder.Close()
	if closeErr := d.pool.Close(); err == nil {
		err = closeErr
	}
	return err
}
