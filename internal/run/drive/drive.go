// Package drive binds typed flow contracts once while opening a Program.
// Items remain typed through every delivery, fan-out, and bounded edge.
package drive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"

	"github.com/godexture/godec/flow"
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
)

type Kind uint8

const (
	Source Kind = iota + 1
	Processor
	Sink
)

func (k Kind) Valid() bool { return k >= Source && k <= Sink }

type port struct {
	id     string
	schema schema.Descriptor
}

type Binding struct {
	kind       Kind
	input      port
	output     port
	openSink   func(flow.Operator) (Link, error)
	prepend    func(flow.Operator, Link) (Link, error)
	openSource func(flow.Operator, Link) (Task, error)
	fanout     func([]Link) (Link, error)
	buffer     func(queue.Limit, Link) (Link, Task, error)
	validate   func(flow.Operator) error
}

func NewSource[T any](output string, typ schema.Type[T]) Binding {
	traits := typ.Traits()
	return Binding{
		kind:   Source,
		output: port{id: output, schema: typ.Descriptor()},
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
		fanout: fanoutFactory(traits),
		buffer: bufferFactory(traits),
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
	return Binding{
		kind:   Processor,
		input:  port{id: input, schema: in.Descriptor()},
		output: port{id: output, schema: out.Descriptor()},
		prepend: func(operator flow.Operator, next Link) (Link, error) {
			processor, ok := operator.(flow.Processor[I, O])
			if !ok {
				return Link{}, fmt.Errorf("%w: want flow.Processor[%s,%s], got %T", ErrOperator, reflect.TypeFor[I](), reflect.TypeFor[O](), operator)
			}
			target, err := deliveryOf[O](next)
			if err != nil {
				return Link{}, err
			}
			return linkOf[I](&processorDelivery[I, O]{processor: processor, next: target}), nil
		},
		fanout: fanoutFactory(traits),
		buffer: bufferFactory(traits),
		validate: func(operator flow.Operator) error {
			if _, ok := operator.(flow.Processor[I, O]); !ok {
				return fmt.Errorf("%w: want flow.Processor[%s,%s], got %T", ErrOperator, reflect.TypeFor[I](), reflect.TypeFor[O](), operator)
			}
			return nil
		},
	}
}

func NewSink[T any](input string, typ schema.Type[T]) Binding {
	return Binding{
		kind:  Sink,
		input: port{id: input, schema: typ.Descriptor()},
		openSink: func(operator flow.Operator) (Link, error) {
			writer, ok := operator.(flow.Writer[T])
			if !ok {
				return Link{}, fmt.Errorf("%w: want flow.Writer[%s], got %T", ErrOperator, reflect.TypeFor[T](), operator)
			}
			return linkOf[T](writerDelivery[T]{writer: writer}), nil
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
		return validPort(b.output) && b.openSource != nil && b.fanout != nil && b.buffer != nil && b.validate != nil
	case Processor:
		return validPort(b.input) && validPort(b.output) && b.prepend != nil && b.fanout != nil && b.buffer != nil && b.validate != nil
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
	case Sink:
		if len(shape.Inputs) != 1 || len(shape.Outputs) != 0 || !matches(shape.Inputs[0], b.input) {
			return ErrBinding
		}
	}
	return nil
}

func (b Binding) OpenSink(operator flow.Operator) (Link, error) {
	if b.openSink == nil {
		return Link{}, ErrUnsupported
	}
	return b.openSink(operator)
}

func (b Binding) Prepend(operator flow.Operator, next Link) (Link, error) {
	if b.prepend == nil {
		return Link{}, ErrUnsupported
	}
	return b.prepend(operator, next)
}

func (b Binding) OpenSource(operator flow.Operator, next Link) (Task, error) {
	if b.openSource == nil {
		return Task{}, ErrUnsupported
	}
	return b.openSource(operator, next)
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

func validPort(value port) bool { return value.id != "" && value.schema.Valid() }

func matches(declared flow.Port, runtime port) bool {
	return declared.ID() == runtime.id &&
		declared.Schema().Identity() == runtime.schema.Identity() &&
		declared.Schema().Payload() == runtime.schema.Payload()
}

type closer interface {
	close(context.Context) error
}

type delivery[T any] interface {
	flow.Emitter[T]
	closer
}

// Link is an opaque typed delivery endpoint used only while Program is
// specialized. Its payload never becomes any during delivery.
type Link struct {
	payload reflect.Type
	value   any
}

func (l Link) Valid() bool { return l.payload != nil && l.value != nil }

func (l Link) Close(ctx context.Context) error {
	if !l.Valid() {
		return ErrLink
	}
	return l.value.(closer).close(ctx)
}

func linkOf[T any](value delivery[T]) Link {
	return Link{payload: reflect.TypeFor[T](), value: value}
}

func deliveryOf[T any](link Link) (delivery[T], error) {
	if link.payload != reflect.TypeFor[T]() {
		return nil, fmt.Errorf("%w: want %s, got %v", ErrLink, reflect.TypeFor[T](), link.payload)
	}
	value, ok := link.value.(delivery[T])
	if !ok {
		return nil, fmt.Errorf("%w: link contains %T", ErrLink, link.value)
	}
	return value, nil
}

// Task is a top-level execution loop. Host runs it through the tracked task
// group, which owns panic recovery, cancellation, and join.
type Task struct {
	run   func(context.Context) error
	close func()
}

func (t Task) Valid() bool { return t.run != nil }

func (t Task) Run(ctx context.Context) error {
	if t.run == nil {
		return ErrBinding
	}
	return t.run(ctx)
}

func (t Task) Close() {
	if t.close != nil {
		t.close()
	}
}

func sourceTask[T any](reader flow.Reader[T], next delivery[T]) Task {
	return Task{run: func(ctx context.Context) error {
		for {
			input, err := reader.Read(ctx)
			if errors.Is(err, io.EOF) {
				if input.Valid() {
					input.Drop()
					return ErrReadWithItem
				}
				return next.close(ctx)
			}
			if err != nil {
				if input.Valid() {
					input.Drop()
					return errors.Join(ErrReadWithItem, err)
				}
				return err
			}
			if !input.Valid() {
				return ErrInvalidItem
			}
			if err := next.Emit(ctx, input); err != nil {
				input.Drop()
				return err
			}
		}
	}}
}

type writerDelivery[T any] struct{ writer flow.Writer[T] }

func (w writerDelivery[T]) Emit(ctx context.Context, input flow.Input[T]) error {
	return w.writer.Write(ctx, input)
}

func (writerDelivery[T]) close(context.Context) error { return nil }

type processorDelivery[I, O any] struct {
	processor flow.Processor[I, O]
	next      delivery[O]
	once      sync.Once
	closeErr  error
}

func (p *processorDelivery[I, O]) Emit(ctx context.Context, input flow.Input[I]) error {
	return p.processor.Process(ctx, input, p.next)
}

func (p *processorDelivery[I, O]) close(ctx context.Context) error {
	p.once.Do(func() {
		p.closeErr = errors.Join(p.processor.Flush(ctx, p.next), p.next.close(ctx))
	})
	return p.closeErr
}

type fanoutDelivery[T any] struct {
	outputs  []delivery[T]
	values   []T
	fork     func(T) T
	drop     func(T)
	once     sync.Once
	closeErr error
}

func fanoutFactory[T any](traits schema.Traits[T]) func([]Link) (Link, error) {
	return func(links []Link) (Link, error) {
		if len(links) == 0 {
			return Link{}, ErrLink
		}
		if len(links) == 1 {
			if _, err := deliveryOf[T](links[0]); err != nil {
				return Link{}, err
			}
			return links[0], nil
		}
		if traits.Drop != nil && traits.Fork == nil {
			return Link{}, ErrForkTrait
		}
		outputs := make([]delivery[T], len(links))
		for index, link := range links {
			output, err := deliveryOf[T](link)
			if err != nil {
				return Link{}, err
			}
			outputs[index] = output
		}
		return linkOf[T](&fanoutDelivery[T]{
			outputs: outputs,
			values:  make([]T, len(outputs)),
			fork:    traits.Fork,
			drop:    traits.Drop,
		}), nil
	}
}

func (f *fanoutDelivery[T]) Emit(ctx context.Context, input flow.Input[T]) error {
	value := input.Value()
	for index := range f.values {
		f.values[index] = value
		if f.fork != nil {
			f.values[index] = f.fork(value)
		}
	}
	for index, output := range f.outputs {
		branch := flow.NewInputWithTraits(f.values[index], f.fork, f.drop)
		if err := output.Emit(ctx, branch); err != nil {
			branch.Drop()
			for remaining := index + 1; remaining < len(f.values); remaining++ {
				if f.drop != nil {
					f.drop(f.values[remaining])
				}
			}
			f.clearValues()
			return err
		}
	}
	f.clearValues()
	input.Drop()
	return nil
}

func (f *fanoutDelivery[T]) close(ctx context.Context) error {
	f.once.Do(func() {
		problems := make([]error, 0, len(f.outputs))
		for _, output := range f.outputs {
			if err := output.close(ctx); err != nil {
				problems = append(problems, err)
			}
		}
		f.closeErr = errors.Join(problems...)
	})
	return f.closeErr
}

func (f *fanoutDelivery[T]) clearValues() {
	var zero T
	for index := range f.values {
		f.values[index] = zero
	}
}

type bufferDelivery[T any] struct{ queue *queue.Queue[flow.Input[T]] }

func (b bufferDelivery[T]) Emit(ctx context.Context, input flow.Input[T]) error {
	return b.queue.Push(ctx, input)
}

func (b bufferDelivery[T]) close(context.Context) error {
	b.queue.Close()
	return nil
}

func bufferFactory[T any](traits schema.Traits[T]) func(queue.Limit, Link) (Link, Task, error) {
	return func(limit queue.Limit, next Link) (Link, Task, error) {
		target, err := deliveryOf[T](next)
		if err != nil {
			return Link{}, Task{}, err
		}
		queueTraits := queue.Traits[flow.Input[T]]{Drop: func(input flow.Input[T]) { input.Drop() }}
		if traits.Size != nil {
			queueTraits.Size = func(input flow.Input[T]) int { return traits.Size(input.Value()) }
		}
		if traits.Time != nil {
			queueTraits.Time = func(input flow.Input[T]) (int64, bool) { return traits.Time(input.Value()) }
		}
		edge, err := queue.New(limit, queueTraits)
		if err != nil {
			return Link{}, Task{}, err
		}
		task := Task{
			close: edge.Close,
			run: func(ctx context.Context) error {
				defer edge.Drain()
				for {
					input, err := edge.Pop(ctx)
					if errors.Is(err, io.EOF) {
						return target.close(ctx)
					}
					if err != nil {
						return err
					}
					if err := target.Emit(ctx, input); err != nil {
						input.Drop()
						return err
					}
				}
			},
		}
		return linkOf[T](bufferDelivery[T]{queue: edge}), task, nil
	}
}
