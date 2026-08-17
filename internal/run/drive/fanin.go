package drive

import (
	"fmt"
	"reflect"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/run/queue"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/timing"
)

// JoinInput is one physical stream entering a fan-in. Its queue limit and
// time base remain local to that stream; a merge compares timestamps exactly
// across the retained bases.
type JoinInput struct {
	Limit queue.Limit
	Base  timing.Base
}

func NewJoiner[I, O any](input string, in schema.Type[I], policy flow.FanInPolicy, output string, out schema.Type[O]) Binding {
	traits := out.Traits()
	inputTraits := in.Traits()
	return Binding{
		kind:        Joiner,
		input:       port{id: input, schema: in.Descriptor()},
		output:      port{id: output, schema: out.Descriptor()},
		inputStats:  measuresOf(inputTraits),
		outputStats: measuresOf(traits),
		inputOrder:  inputTraits.Order != nil,
		fanIn:       policy,
		openJoiner: func(operator flow.Operator, inputs []JoinInput, tolerance int64, next Link, owner *journal.Domain) ([]Link, Task, error) {
			joiner, ok := operator.(flow.Joiner[I, O])
			if !ok {
				return nil, Task{}, fmt.Errorf("%w: want flow.Joiner[%s,%s], got %T", ErrOperator, reflect.TypeFor[I](), reflect.TypeFor[O](), operator)
			}
			target, err := deliveryOf[O](next)
			if err != nil {
				return nil, Task{}, err
			}
			switch policy {
			case flow.ZipFanIn:
				return zipJoiner(joiner, inputs, tolerance, in, target, owner)
			case flow.MergeFanIn:
				return mergeJoiner(joiner, inputs, tolerance, in, target, owner)
			default:
				return nil, Task{}, ErrUnsupported
			}
		},
		fanout:  fanoutFactory(out),
		buffer:  bufferFactory(out),
		observe: observeFactory(out),
		validate: func(operator flow.Operator) error {
			if _, ok := operator.(flow.Joiner[I, O]); !ok {
				return fmt.Errorf("%w: want flow.Joiner[%s,%s], got %T", ErrOperator, reflect.TypeFor[I](), reflect.TypeFor[O](), operator)
			}
			return nil
		},
	}
}

func (b Binding) OpenJoiner(operator flow.Operator, inputs []JoinInput, tolerance int64, next Link, owner *journal.Domain) ([]Link, Task, error) {
	if b.openJoiner == nil {
		return nil, Task{}, ErrUnsupported
	}
	if tolerance < 0 || tolerance > 0 && (b.fanIn != flow.ZipFanIn || !b.inputStats.Time) {
		return nil, Task{}, ErrBinding
	}
	if owner == nil {
		return nil, Task{}, ErrDomain
	}
	next.bind(owner)
	return b.openJoiner(operator, inputs, tolerance, next, owner)
}

func (b Binding) InputOrder() bool { return b.inputOrder }
