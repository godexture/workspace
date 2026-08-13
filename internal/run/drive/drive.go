// Package drive binds typed flow contracts once while opening a Program.
// Items remain typed through every delivery, fan-out, and bounded edge.
package drive

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/observe"
	"github.com/godexture/godec/internal/run/queue"
	"github.com/godexture/godec/media/schema"
)

var (
	ErrBinding      = errors.New("component has no compatible typed execution binding")
	ErrOperator     = errors.New("opened operator does not implement its typed execution contract")
	ErrLink         = errors.New("runtime link payload type does not match")
	ErrInvalidItem  = errors.New("reader returned an invalid owned item")
	ErrReadWithItem = errors.New("reader returned an owned item together with an error")
	ErrUnsupported  = errors.New("execution binding does not support this operation")
	ErrForkTrait    = errors.New("owned fan-out requires a fork trait")
	ErrWatermark    = errors.New("fan-in batch exceeds its timestamp watermark")
)

type Kind uint8

const (
	Source Kind = iota + 1
	Processor
	Joiner
	Sink
)

func (k Kind) Valid() bool { return k >= Source && k <= Sink }

type port struct {
	id     string
	schema schema.Descriptor
}

type Measures struct {
	Size bool
	Time bool
}

type Binding struct {
	kind        Kind
	input       port
	output      port
	inputStats  Measures
	outputStats Measures
	fanIn       flow.FanInPolicy
	openSink    func(flow.Operator, string) (Link, error)
	prepend     func(flow.Operator, Link, string) (Link, error)
	openSource  func(flow.Operator, Link) (Task, error)
	fanout      func([]Link) (Link, error)
	buffer      func(queue.Limit, Link) (Link, Task, error)
	observe     func(Link, *observe.Local) (Link, error)
	openJoiner  func(flow.Operator, int, queue.Limit, Link) ([]Link, Task, error)
	validate    func(flow.Operator) error
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
		fanIn:       policy,
		openJoiner: func(operator flow.Operator, inputs int, limit queue.Limit, next Link) ([]Link, Task, error) {
			joiner, ok := operator.(flow.Joiner[I, O])
			if !ok {
				return nil, Task{}, fmt.Errorf("%w: want flow.Joiner[%s,%s], got %T", ErrOperator, reflect.TypeFor[I](), reflect.TypeFor[O](), operator)
			}
			target, err := deliveryOf[O](next)
			if err != nil {
				return nil, Task{}, err
			}
			if policy != flow.ZipFanIn {
				return nil, Task{}, ErrUnsupported
			}
			return zipJoiner(joiner, inputs, limit, inputTraits, target)
		},
		fanout:  fanoutFactory(traits),
		buffer:  bufferFactory(traits),
		observe: observeFactory(traits),
		validate: func(operator flow.Operator) error {
			if _, ok := operator.(flow.Joiner[I, O]); !ok {
				return fmt.Errorf("%w: want flow.Joiner[%s,%s], got %T", ErrOperator, reflect.TypeFor[I](), reflect.TypeFor[O](), operator)
			}
			return nil
		},
	}
}

func NewSource[T any](output string, typ schema.Type[T]) Binding {
	traits := typ.Traits()
	return Binding{
		kind:        Source,
		output:      port{id: output, schema: typ.Descriptor()},
		outputStats: measuresOf(traits),
		openSource: func(operator flow.Operator, next Link) (Task, error) {
			reader, ok := operator.(flow.Reader[T])
			if !ok {
				return Task{}, fmt.Errorf("%w: want flow.Reader[%s], got %T", ErrOperator, reflect.TypeFor[T](), operator)
			}
			target, err := deliveryOf[T](next)
			if err != nil {
				return Task{}, err
			}
			return sourceTask(reader, target), nil
		},
		fanout:  fanoutFactory(traits),
		buffer:  bufferFactory(traits),
		observe: observeFactory(traits),
		validate: func(operator flow.Operator) error {
			if _, ok := operator.(flow.Reader[T]); !ok {
				return fmt.Errorf("%w: want flow.Reader[%s], got %T", ErrOperator, reflect.TypeFor[T](), operator)
			}
			return nil
		},
	}
}

func NewProcessor[I, O any](input string, in schema.Type[I], output string, out schema.Type[O]) Binding {
	traits := out.Traits()
	inputTraits := in.Traits()
	return Binding{
		kind:        Processor,
		input:       port{id: input, schema: in.Descriptor()},
		output:      port{id: output, schema: out.Descriptor()},
		inputStats:  measuresOf(inputTraits),
		outputStats: measuresOf(traits),
		prepend: func(operator flow.Operator, next Link, node string) (Link, error) {
			processor, ok := operator.(flow.Processor[I, O])
			if !ok {
				return Link{}, fmt.Errorf("%w: want flow.Processor[%s,%s], got %T", ErrOperator, reflect.TypeFor[I](), reflect.TypeFor[O](), operator)
			}
			target, err := deliveryOf[O](next)
			if err != nil {
				return Link{}, err
			}
			return linkOf[I](&processorDelivery[I, O]{processor: processor, next: target, node: node}), nil
		},
		fanout:  fanoutFactory(traits),
		buffer:  bufferFactory(traits),
		observe: observeFactory(traits),
		validate: func(operator flow.Operator) error {
			if _, ok := operator.(flow.Processor[I, O]); !ok {
				return fmt.Errorf("%w: want flow.Processor[%s,%s], got %T", ErrOperator, reflect.TypeFor[I](), reflect.TypeFor[O](), operator)
			}
			return nil
		},
	}
}

func NewSink[T any](input string, typ schema.Type[T]) Binding {
	traits := typ.Traits()
	return Binding{
		kind:       Sink,
		input:      port{id: input, schema: typ.Descriptor()},
		inputStats: measuresOf(traits),
		openSink: func(operator flow.Operator, node string) (Link, error) {
			writer, ok := operator.(flow.Writer[T])
			if !ok {
				return Link{}, fmt.Errorf("%w: want flow.Writer[%s], got %T", ErrOperator, reflect.TypeFor[T](), operator)
			}
			return linkOf[T](&writerDelivery[T]{writer: writer, node: node}), nil
		},
		validate: func(operator flow.Operator) error {
			if _, ok := operator.(flow.Writer[T]); !ok {
				return fmt.Errorf("%w: want flow.Writer[%s], got %T", ErrOperator, reflect.TypeFor[T](), operator)
			}
			return nil
		},
	}
}

func (b Binding) Valid() bool {
	if !b.kind.Valid() {
		return false
	}
	switch b.kind {
	case Source:
		return validPort(b.output) && b.openSource != nil && b.fanout != nil && b.buffer != nil && b.observe != nil && b.validate != nil
	case Processor:
		return validPort(b.input) && validPort(b.output) && b.prepend != nil && b.fanout != nil && b.buffer != nil && b.observe != nil && b.validate != nil
	case Joiner:
		return validPort(b.input) && validPort(b.output) && b.openJoiner != nil && b.fanout != nil && b.buffer != nil && b.observe != nil && b.validate != nil
	case Sink:
		return validPort(b.input) && b.openSink != nil && b.validate != nil
	default:
		return false
	}
}

func (b Binding) Kind() Kind { return b.kind }

func (b Binding) ValidateOperator(operator flow.Operator) error {
	if !b.Valid() || operator == nil {
		return ErrBinding
	}
	return b.validate(operator)
}

func (b Binding) Validate(shape flow.Shape) error {
	if !b.Valid() {
		return ErrBinding
	}
	switch b.kind {
	case Source:
		if len(shape.Inputs) != 0 || len(shape.Outputs) != 1 || !matches(shape.Outputs[0], b.output) {
			return ErrBinding
		}
	case Processor:
		if len(shape.Inputs) != 1 || len(shape.Outputs) != 1 || !matches(shape.Inputs[0], b.input) || !matches(shape.Outputs[0], b.output) {
			return ErrBinding
		}
	case Joiner:
		if len(shape.Inputs) != 1 || len(shape.Outputs) != 1 || shape.Inputs[0].Multiplicity() != flow.ManyMultiplicity || shape.Inputs[0].FanIn() != b.fanIn || !matches(shape.Inputs[0], b.input) || !matches(shape.Outputs[0], b.output) {
			return ErrBinding
		}
	case Sink:
		if len(shape.Inputs) != 1 || len(shape.Outputs) != 0 || !matches(shape.Inputs[0], b.input) {
			return ErrBinding
		}
	}
	return nil
}

func (b Binding) Input() string            { return b.input.id }
func (b Binding) Output() string           { return b.output.id }
func (b Binding) FanIn() flow.FanInPolicy  { return b.fanIn }
func (b Binding) InputMeasures() Measures  { return b.inputStats }
func (b Binding) OutputMeasures() Measures { return b.outputStats }

func measuresOf[T any](traits schema.Traits[T]) Measures {
	return Measures{Size: traits.Size != nil, Time: traits.Time != nil}
}

func (b Binding) OpenSink(operator flow.Operator) (Link, error) {
	return b.OpenSinkAt(operator, "")
}

func (b Binding) OpenSinkAt(operator flow.Operator, node string) (Link, error) {
	if b.openSink == nil {
		return Link{}, ErrUnsupported
	}
	return b.openSink(operator, node)
}

func (b Binding) Prepend(operator flow.Operator, next Link) (Link, error) {
	return b.PrependAt(operator, next, "")
}

func (b Binding) PrependAt(operator flow.Operator, next Link, node string) (Link, error) {
	if b.prepend == nil {
		return Link{}, ErrUnsupported
	}
	return b.prepend(operator, next, node)
}

func (b Binding) OpenSource(operator flow.Operator, next Link) (Task, error) {
	if b.openSource == nil {
		return Task{}, ErrUnsupported
	}
	return b.openSource(operator, next)
}

func (b Binding) OpenJoiner(operator flow.Operator, inputs int, limit queue.Limit, next Link) ([]Link, Task, error) {
	if b.openJoiner == nil {
		return nil, Task{}, ErrUnsupported
	}
	return b.openJoiner(operator, inputs, limit, next)
}

func (b Binding) Fanout(outputs []Link) (Link, error) {
	if b.fanout == nil {
		return Link{}, ErrUnsupported
	}
	return b.fanout(outputs)
}

func (b Binding) Buffer(limit queue.Limit, next Link) (Link, Task, error) {
	if b.buffer == nil {
		return Link{}, Task{}, ErrUnsupported
	}
	return b.buffer(limit, next)
}

func (b Binding) Observe(next Link, local *observe.Local) (Link, error) {
	if local == nil {
		return next, nil
	}
	if b.observe == nil {
		return Link{}, ErrUnsupported
	}
	return b.observe(next, local)
}

func validPort(value port) bool { return value.id != "" && value.schema.Valid() }

func matches(declared flow.Port, runtime port) bool {
	return declared.ID() == runtime.id &&
		declared.Schema().Identity() == runtime.schema.Identity() &&
		declared.Schema().Payload() == runtime.schema.Payload()
}
