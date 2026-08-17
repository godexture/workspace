// Package drive binds typed flow contracts once while opening a Program.
// Items remain typed through every delivery, fan-out, and bounded edge.
package drive

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
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
	ErrTolerance    = errors.New("fan-in batch exceeds its timestamp tolerance")
	ErrDomain       = errors.New("execution task requires the failure domain it and its slots report to")
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
	openSource  func(flow.Operator, Link, *journal.Domain) (Task, error)
	fanout      func([]Link, string) (Link, error)
	buffer      func(queue.Limit, Link, *journal.Domain) (Link, Task, error)
	observe     func(Link, *observe.Local) (Link, error)
	openJoiner  func(flow.Operator, int, queue.Limit, int64, Link, *journal.Domain) ([]Link, Task, error)
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
		openJoiner: func(operator flow.Operator, inputs int, limit queue.Limit, tolerance int64, next Link, owner *journal.Domain) ([]Link, Task, error) {
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
			return zipJoiner(joiner, inputs, limit, tolerance, in, target, owner)
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

func NewSource[T any](output string, typ schema.Type[T]) Binding {
	traits := typ.Traits()
	return Binding{
		kind:        Source,
		output:      port{id: output, schema: typ.Descriptor()},
		outputStats: measuresOf(traits),
		openSource: func(operator flow.Operator, next Link, owner *journal.Domain) (Task, error) {
			reader, ok := operator.(flow.Reader[T])
			if !ok {
				return Task{}, fmt.Errorf("%w: want flow.Reader[%s], got %T", ErrOperator, reflect.TypeFor[T](), operator)
			}
			target, err := deliveryOf[T](next)
			if err != nil {
				return Task{}, err
			}
			return sourceTask(reader, typ, target, owner), nil
		},
		fanout:  fanoutFactory(typ),
		buffer:  bufferFactory(typ),
		observe: observeFactory(typ),
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
			return linkOf[I](&processorDelivery[I, O]{processor: processor, next: target, typ: in, node: node}), nil
		},
		fanout:  fanoutFactory(out),
		buffer:  bufferFactory(out),
		observe: observeFactory(out),
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
			return linkOf[T](&writerDelivery[T]{writer: writer, typ: typ, node: node}), nil
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

// OpenSource, OpenJoiner and Buffer take the failure domain their task will
// own and bind the whole chain below it before returning. The domain is a
// construction argument rather than a second call beside the Task, and no
// constructor invents one of its own, so a task that exists is a task whose
// every slot reports somewhere the run collects from.
func (b Binding) OpenSource(operator flow.Operator, next Link, owner *journal.Domain) (Task, error) {
	if b.openSource == nil {
		return Task{}, ErrUnsupported
	}
	if owner == nil {
		return Task{}, ErrDomain
	}
	next.bind(owner)
	return b.openSource(operator, next, owner)
}

func (b Binding) OpenJoiner(operator flow.Operator, inputs int, limit queue.Limit, tolerance int64, next Link, owner *journal.Domain) ([]Link, Task, error) {
	if b.openJoiner == nil {
		return nil, Task{}, ErrUnsupported
	}
	if tolerance < 0 {
		return nil, Task{}, ErrBinding
	}
	if owner == nil {
		return nil, Task{}, ErrDomain
	}
	next.bind(owner)
	return b.openJoiner(operator, inputs, limit, tolerance, next, owner)
}

// Fanout groups this node's outputs. The branch slots it retains belong to
// whichever task ends up driving them, so it takes only the node it attributes
// their releases to; the domain arrives when that task's constructor binds the
// chain.
func (b Binding) Fanout(outputs []Link, node string) (Link, error) {
	if b.fanout == nil {
		return Link{}, ErrUnsupported
	}
	return b.fanout(outputs, node)
}

func (b Binding) Buffer(limit queue.Limit, next Link, owner *journal.Domain) (Link, Task, error) {
	if b.buffer == nil {
		return Link{}, Task{}, ErrUnsupported
	}
	if owner == nil {
		return Link{}, Task{}, ErrDomain
	}
	next.bind(owner)
	return b.buffer(limit, next, owner)
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
		declared.Schema().Equal(runtime.schema)
}
