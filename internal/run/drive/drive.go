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

type closer interface {
	close(context.Context) error
}

type scopeBinder interface{ bindScope(*Scope) }

// Scope is task-local panic context. It intentionally uses no atomics because
// the owning execution task is the only writer and panic recovery reads it in
// that same goroutine. It carries no item cleanup: every cell is released by
// the deferred Drop of the task that owns it, which also runs while a panic
// unwinds.
type Scope struct {
	node string
}

func NewScope(node string) *Scope { return &Scope{node: node} }
func (s *Scope) Node() string {
	if s == nil {
		return ""
	}
	return s.node
}
func (s *Scope) set(node string) {
	if s != nil {
		s.node = node
	}
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

func (l Link) BindScope(scope *Scope) {
	if !l.Valid() {
		return
	}
	if value, ok := l.value.(scopeBinder); ok {
		value.bindScope(scope)
	}
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
	run     func(context.Context) error
	barrier func(context.Context) error
	finish  func(context.Context) error
	close   func()
	discard func()
	bind    func(*Scope)
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

// Discard releases queued owners after every producer and consumer using the
// task has joined. It is deliberately separate from Close: closing wakes
// tasks, while discarding is only race-free after they have stopped.
func (t Task) Discard() {
	if t.discard != nil {
		t.discard()
	}
}

func (t Task) Finish(ctx context.Context) error {
	if t.finish == nil {
		return nil
	}
	return t.finish(ctx)
}

func (t Task) Barrier(ctx context.Context) error {
	if t.barrier == nil {
		return nil
	}
	return t.barrier(ctx)
}

func (t Task) BindScope(scope *Scope) {
	if t.bind != nil {
		t.bind(scope)
	}
}

// sourceTask reuses one ownership cell for the whole stream. Anything the
// chain leaves unconsumed — including during panic unwinding — is released by
// the deferred Drop, so no stage needs a failure-path ownership rule.
func sourceTask[T any](reader flow.Reader[T], next delivery[T]) Task {
	return Task{finish: next.close, run: func(ctx context.Context) error {
		var item flow.Item[T]
		defer item.Drop()
		for {
			err := reader.Read(ctx, &item)
			if errors.Is(err, io.EOF) {
				if item.Valid() {
					return ErrReadWithItem
				}
				return nil
			}
			if err != nil {
				if item.Valid() {
					return errors.Join(ErrReadWithItem, err)
				}
				return err
			}
			if !item.Valid() {
				return ErrInvalidItem
			}
			if err := next.Emit(ctx, &item); err != nil {
				return err
			}
		}
	}}
}

type writerDelivery[T any] struct {
	writer flow.Writer[T]
	node   string
	scope  *Scope
}

func (w *writerDelivery[T]) Emit(ctx context.Context, item *flow.Item[T]) error {
	previous := w.scope.Node()
	w.scope.set(w.node)
	err := w.writer.Write(ctx, item)
	w.scope.set(previous)
	return err
}

func (*writerDelivery[T]) close(context.Context) error { return nil }
func (w *writerDelivery[T]) bindScope(scope *Scope)    { w.scope = scope }

type processorDelivery[I, O any] struct {
	processor flow.Processor[I, O]
	next      delivery[O]
	node      string
	scope     *Scope
	once      sync.Once
	closeErr  error
}

func (p *processorDelivery[I, O]) Emit(ctx context.Context, item *flow.Item[I]) error {
	previous := p.scope.Node()
	p.scope.set(p.node)
	err := p.processor.Process(ctx, item, p.next)
	p.scope.set(previous)
	return err
}

func (p *processorDelivery[I, O]) close(ctx context.Context) error {
	p.once.Do(func() {
		previous := p.scope.Node()
		p.scope.set(p.node)
		p.closeErr = errors.Join(p.processor.Flush(ctx, p.next), p.next.close(ctx))
		p.scope.set(previous)
	})
	return p.closeErr
}

func (p *processorDelivery[I, O]) bindScope(scope *Scope) {
	p.scope = scope
	if next, ok := p.next.(scopeBinder); ok {
		next.bindScope(scope)
	}
}

type fanoutDelivery[T any] struct {
	outputs  []delivery[T]
	branches []flow.Item[T]
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
			outputs:  outputs,
			branches: make([]flow.Item[T], len(outputs)-1),
		}), nil
	}
}

// Emit forks one owner per extra output and hands the original to the last
// one. Branch cells are reused across items, and the deferred release covers
// every failure and panic path without per-branch bookkeeping.
func (f *fanoutDelivery[T]) Emit(ctx context.Context, item *flow.Item[T]) error {
	defer f.dropBranches()
	for index := range f.branches {
		if !item.Fork(&f.branches[index]) {
			return ErrInvalidItem
		}
	}
	for index := range f.branches {
		if err := f.outputs[index].Emit(ctx, &f.branches[index]); err != nil {
			return err
		}
	}
	return f.outputs[len(f.outputs)-1].Emit(ctx, item)
}

func (f *fanoutDelivery[T]) dropBranches() {
	for index := range f.branches {
		f.branches[index].Drop()
	}
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

func (f *fanoutDelivery[T]) bindScope(scope *Scope) {
	for _, output := range f.outputs {
		if value, ok := output.(scopeBinder); ok {
			value.bindScope(scope)
		}
	}
}

type bufferDelivery[T any] struct {
	queue *queue.Queue[flow.Owned[T]]
}

// Emit detaches ownership into the queue. A rejected push never stores the
// value, so the cell takes it back and the producer stays responsible.
func (b *bufferDelivery[T]) Emit(ctx context.Context, item *flow.Item[T]) error {
	owned := item.Consume()
	if err := b.queue.Push(ctx, owned); err != nil {
		item.Adopt(owned)
		return err
	}
	return nil
}

func (b *bufferDelivery[T]) close(context.Context) error {
	b.queue.Close()
	return nil
}

func bufferFactory[T any](traits schema.Traits[T]) func(queue.Limit, Link) (Link, Task, error) {
	return func(limit queue.Limit, next Link) (Link, Task, error) {
		target, err := deliveryOf[T](next)
		if err != nil {
			return Link{}, Task{}, err
		}
		edge, err := queue.New(limit, ownedTraits(traits))
		if err != nil {
			return Link{}, Task{}, err
		}
		task := Task{
			close:   edge.Close,
			discard: func() { edge.Drain() },
			barrier: edge.WaitIdle,
			run: func(ctx context.Context) error {
				defer edge.Drain()
				var item flow.Item[T]
				defer item.Drop()
				for {
					owned, err := edge.Pop(ctx)
					if errors.Is(err, io.EOF) {
						return target.close(ctx)
					}
					if err != nil {
						return err
					}
					item.Adopt(owned)
					emitErr := target.Emit(ctx, &item)
					edge.Complete()
					if emitErr != nil {
						return emitErr
					}
				}
			},
		}
		return linkOf[T](&bufferDelivery[T]{queue: edge}), task, nil
	}
}

// ownedTraits projects schema traits onto detached ownership so a queue can
// size, time, and release what it holds without knowing about cells.
func ownedTraits[T any](traits schema.Traits[T]) queue.Traits[flow.Owned[T]] {
	result := queue.Traits[flow.Owned[T]]{Drop: func(value flow.Owned[T]) { value.Release() }}
	if traits.Size != nil {
		result.Size = func(value flow.Owned[T]) int { return traits.Size(value.Value()) }
	}
	if traits.Time != nil {
		result.Time = func(value flow.Owned[T]) (int64, bool) { return traits.Time(value.Value()) }
	}
	return result
}

type observedDelivery[T any] struct {
	next  delivery[T]
	local *observe.Local
	size  func(T) int
	time  func(T) (int64, bool)
}

func (o *observedDelivery[T]) Emit(ctx context.Context, item *flow.Item[T]) error {
	var bytes uint64
	if o.size != nil {
		if value := o.size(item.Value()); value > 0 {
			bytes = uint64(value)
		}
	}
	var media int64
	var timed bool
	if o.time != nil {
		media, timed = o.time(item.Value())
	}
	if err := o.next.Emit(ctx, item); err != nil {
		return err
	}
	o.local.Add(bytes, media, timed)
	return nil
}

func (o *observedDelivery[T]) close(ctx context.Context) error {
	err := o.next.close(ctx)
	o.local.Flush()
	return err
}

func (o *observedDelivery[T]) bindScope(scope *Scope) {
	if next, ok := o.next.(scopeBinder); ok {
		next.bindScope(scope)
	}
}

func observeFactory[T any](traits schema.Traits[T]) func(Link, *observe.Local) (Link, error) {
	return func(next Link, local *observe.Local) (Link, error) {
		target, err := deliveryOf[T](next)
		if err != nil {
			return Link{}, err
		}
		result := &observedDelivery[T]{next: target, local: local}
		if local.Detailed() {
			result.size = traits.Size
			result.time = traits.Time
		}
		return linkOf[T](result), nil
	}
}

func zipJoiner[I, O any](joiner flow.Joiner[I, O], count int, limit queue.Limit, traits schema.Traits[I], next delivery[O]) ([]Link, Task, error) {
	if count < 2 {
		return nil, Task{}, ErrBinding
	}
	edges := make([]*queue.Queue[flow.Owned[I]], count)
	links := make([]Link, count)
	queueTraits := ownedTraits(traits)
	for index := range edges {
		edge, err := queue.New(limit, queueTraits)
		if err != nil {
			for previous := 0; previous < index; previous++ {
				edges[previous].Close()
			}
			return nil, Task{}, err
		}
		edges[index] = edge
		links[index] = linkOf[I](&bufferDelivery[I]{queue: edge})
	}
	state := &zipState[I, O]{joiner: joiner, edges: edges, items: make([]flow.Item[I], count), batch: make([]*flow.Item[I], count), next: next, time: traits.Time, watermark: limit.Time, done: make(chan struct{})}
	for index := range state.items {
		state.batch[index] = &state.items[index]
	}
	task := Task{
		close:   state.close,
		discard: state.discard,
		barrier: state.barrier,
		finish:  state.finish,
		run:     state.run,
	}
	return links, task, nil
}

type zipState[I, O any] struct {
	joiner    flow.Joiner[I, O]
	edges     []*queue.Queue[flow.Owned[I]]
	items     []flow.Item[I]
	batch     []*flow.Item[I]
	read      int
	next      delivery[O]
	time      func(I) (int64, bool)
	watermark int64
	done      chan struct{}
	once      sync.Once
	err       error
}

func (s *zipState[I, O]) close() {
	for _, edge := range s.edges {
		edge.Close()
	}
}

func (s *zipState[I, O]) discard() {
	for _, edge := range s.edges {
		edge.Drain()
	}
}

func (s *zipState[I, O]) run(ctx context.Context) error {
	defer func() {
		s.close()
		for _, edge := range s.edges {
			edge.Drain()
		}
		close(s.done)
	}()
	defer s.release()
	for {
		s.read = 0
		for index, edge := range s.edges {
			owned, err := edge.Pop(ctx)
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			s.items[index].Adopt(owned)
			s.read++
		}
		if !s.withinWatermark() {
			return ErrWatermark
		}
		if err := s.joiner.Process(ctx, flow.NewBatch(s.batch[:s.read]), s.next); err != nil {
			return err
		}
		s.release()
	}
}

func (s *zipState[I, O]) withinWatermark() bool {
	if s.watermark == 0 {
		return true
	}
	minimum, maximum := int64(0), int64(0)
	for index := 0; index < s.read; index++ {
		value, ok := s.time(s.items[index].Value())
		if !ok {
			return false
		}
		if index == 0 || value < minimum {
			minimum = value
		}
		if index == 0 || value > maximum {
			maximum = value
		}
	}
	return uint64(maximum)-uint64(minimum) <= uint64(s.watermark)
}

// release drops whatever the joiner left behind and returns the group's slots
// to their queues. A Joiner may consume any subset of the batch.
func (s *zipState[I, O]) release() {
	for index := 0; index < s.read; index++ {
		s.items[index].Drop()
		s.edges[index].Complete()
	}
	s.read = 0
}

func (s *zipState[I, O]) barrier(ctx context.Context) error {
	s.close()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *zipState[I, O]) finish(ctx context.Context) error {
	s.once.Do(func() { s.err = errors.Join(s.joiner.Flush(ctx, s.next), s.next.close(ctx)) })
	return s.err
}
