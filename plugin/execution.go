package plugin

import (
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/run/drive"
	"github.com/godexture/godec/media/schema"
)

// WithReader binds a source component's sole output to flow.Reader[T]. The
// typed assertion is performed once during Open; scheduling remains private
// to Host runtime.
func WithReader[T any](output string, typ schema.Type[T]) ComponentOption {
	return withExecution(drive.NewSource(output, typ))
}

// WithProcessor binds a synchronous one-input/one-output component to
// flow.Processor[I,O]. Adjacent compatible processors can be fused without
// exposing an execution island or queue to the plugin.
func WithProcessor[I, O any](input string, in schema.Type[I], output string, out schema.Type[O]) ComponentOption {
	return withExecution(drive.NewProcessor(input, in, output, out))
}

// WithWriter binds a sink component's sole input to flow.Writer[T].
func WithWriter[T any](input string, typ schema.Type[T]) ComponentOption {
	return withExecution(drive.NewSink(input, typ))
}

func withExecution(binding drive.Binding) ComponentOption {
	return func(options *componentOptions) {
		if options.executionSet {
			options.problems = append(options.problems, specItem("plugin.execution", "component may declare only one execution binding"))
			return
		}
		options.executionSet = true
		options.execution = binding
		if !binding.Valid() {
			options.problems = append(options.problems, diagnostic.NewItem("plugin.execution", diagnostic.ErrorSeverity, diagnostic.Path{}, "component execution binding is invalid", nil))
		}
	}
}

// ExecutionOf is the private Program bridge for a compiled typed binding. Its
// token is inspected only by internal/run; plugin and item APIs never receive
// it.
func ExecutionOf(compilation Compilation) (any, bool) {
	if !compilation.executionSet || !compilation.execution.Valid() {
		return nil, false
	}
	return compilation.execution, true
}
