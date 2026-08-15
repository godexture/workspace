package run

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/observe"
	"github.com/godexture/godec/internal/run/drive"
	"github.com/godexture/godec/internal/task"
)

var (
	ErrOperatorCount = errors.New("runtime operator count does not match Program")
	ErrStarted       = errors.New("runtime execution has already started")
)

type namedTask struct {
	name  string
	task  drive.Task
	chain drive.Link
	scope *journal.Scope
	done  chan error
}

// Execution is a one-shot opened data path. It owns only runtime edge state;
// Host retains operator/resource lifecycle ownership.
type Execution struct {
	edges   []namedTask
	sources []namedTask

	mu         sync.Mutex
	started    bool
	closeOnce  sync.Once
	finishOnce sync.Once
	finishErr  error
	finished   []journal.Outcome
	observer   *observe.Collector
}

func (t Template) Build(operators []flow.Operator) (*Execution, error) {
	return t.BuildObserved(operators, nil)
}

func (t Template) BuildObserved(operators []flow.Operator, observer *observe.Collector) (*Execution, error) {
	if !t.executable || len(operators) != len(t.nodes) {
		return nil, ErrOperatorCount
	}
	for index, operator := range operators {
		if operator == nil {
			return nil, ErrOperatorCount
		}
		if err := t.nodes[index].binding.ValidateOperator(operator); err != nil {
			return nil, err
		}
	}
	result := &Execution{observer: observer}
	edgeTargets := make([]drive.Link, len(t.edges))
	fail := func(err error) (*Execution, error) {
		result.Close()
		return nil, err
	}

	for index := len(t.nodes) - 1; index >= 0; index-- {
		value := t.nodes[index]
		operator := operators[index]
		switch value.kind {
		case drive.Sink:
			link, err := value.binding.OpenSinkAt(operator, value.id.String())
			if err != nil {
				return fail(err)
			}
			edgeTargets[t.incoming[index][0]] = link
		case drive.Processor:
			output, err := t.outputLink(index, edgeTargets, result)
			if err != nil {
				return fail(err)
			}
			input, err := value.binding.PrependAt(operator, output, value.id.String())
			if err != nil {
				return fail(err)
			}
			edgeTargets[t.incoming[index][0]] = input
		case drive.Joiner:
			output, err := t.outputLink(index, edgeTargets, result)
			if err != nil {
				return fail(err)
			}
			incoming := append([]int(nil), t.incoming[index]...)
			sort.Slice(incoming, func(left, right int) bool {
				return t.edges[incoming[left]].value.From().String() < t.edges[incoming[right]].value.From().String()
			})
			scope := journal.New(journal.Run, value.id.String())
			output.BindScope(scope)
			inputs, joinTask, err := value.binding.OpenJoiner(operator, len(incoming), t.edges[incoming[0]].limit, output)
			if err != nil {
				return fail(err)
			}
			if len(inputs) != len(incoming) {
				return fail(ErrTopology)
			}
			for inputIndex, edgeIndex := range incoming {
				edgeTargets[edgeIndex] = inputs[inputIndex]
			}
			joinTask.BindScope(scope)
			result.edges = append(result.edges, namedTask{name: "join/" + value.id.String(), task: joinTask, chain: output, scope: scope})
		case drive.Source:
			output, err := t.outputLink(index, edgeTargets, result)
			if err != nil {
				return fail(err)
			}
			scope := journal.New(journal.Run, value.id.String())
			output.BindScope(scope)
			sourceTask, err := value.binding.OpenSource(operator, output)
			if err != nil {
				return fail(err)
			}
			result.sources = append(result.sources, namedTask{name: "source/" + value.id.String(), task: sourceTask, chain: output, scope: scope, done: make(chan error, 1)})
		default:
			return fail(ErrTopology)
		}
	}
	for _, link := range edgeTargets {
		if !link.Valid() {
			return fail(ErrTopology)
		}
	}
	return result, nil
}

func (t Template) outputLink(index int, targets []drive.Link, execution *Execution) (drive.Link, error) {
	links := make([]drive.Link, len(t.outgoing[index]))
	for outputIndex, edgeIndex := range t.outgoing[index] {
		edge := t.edges[edgeIndex]
		link := targets[edgeIndex]
		if !link.Valid() {
			return drive.Link{}, ErrTopology
		}
		local := execution.observer.Local("", edgeKey(edge.value))
		observed, err := t.nodes[index].binding.Observe(link, local)
		if err != nil {
			return drive.Link{}, err
		}
		link = observed
		if edge.reason != 0 && t.nodes[edge.to].kind != drive.Joiner {
			scope := journal.New(journal.Run, "")
			link.BindScope(scope)
			buffered, bufferTask, err := t.nodes[index].binding.Buffer(edge.limit, link)
			if err != nil {
				return drive.Link{}, err
			}
			link = buffered
			bufferTask.BindScope(scope)
			execution.edges = append(execution.edges, namedTask{name: "buffer/" + edgeKey(edge.value), task: bufferTask, scope: scope})
		}
		links[outputIndex] = link
	}
	return t.nodes[index].binding.Fanout(links)
}

// Start registers edge tasks before sources so bounded consumers are ready
// before a Reader publishes its first item.
func (e *Execution) Start(group *task.Group) error {
	if e == nil || group == nil {
		return ErrStarted
	}
	e.mu.Lock()
	if e.started {
		e.mu.Unlock()
		return ErrStarted
	}
	e.started = true
	e.mu.Unlock()
	for _, value := range e.edges {
		if err := group.StartScoped(value.name, value.scope, value.task.Run); err != nil {
			e.Close()
			group.Cancel(err)
			return err
		}
	}
	for _, value := range e.sources {
		current := value
		work := func(ctx context.Context) error {
			err := current.task.Run(ctx)
			current.done <- err
			return err
		}
		if err := group.StartScoped(current.name, current.scope, work); err != nil {
			e.Close()
			group.Cancel(err)
			return err
		}
	}
	return nil
}

// WaitSources waits for Reader EOF without sealing the shared task group;
// edge tasks remain live while Host performs Finalize.
func (e *Execution) WaitSources(ctx context.Context, group *task.Group) error {
	if e == nil || group == nil {
		return ErrStarted
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for index := range e.sources {
		select {
		case err := <-e.sources[index].done:
			if err != nil {
				return err
			}
		case <-group.Context().Done():
			return context.Cause(group.Context())
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Quiesce establishes a data barrier after every Reader has reached
// EOF. Boundaries are visited upstream-to-downstream, so all ordinary Process
// calls finish before Host invokes Finalize while queues remain able to accept
// delayed Flush output.
func (e *Execution) Quiesce(ctx context.Context) error {
	if e == nil {
		return ErrStarted
	}
	for index := len(e.edges) - 1; index >= 0; index-- {
		if err := e.edges[index].task.Barrier(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Finish closes every source edge after successful Finalize. Downstream edge
// tasks then invoke Processor Flush and close in dependency order.
//
// Flushing is its own lifecycle operation, performed by this goroutine, so a
// task whose Finish is a real Host-driven hand-off gets its own journal here
// rather than writing into the run it already sealed. That is only safe for a
// chain no other goroutine can still be touching: a source qualifies, because
// WaitSources already confirmed its own goroutine returned, and a join
// qualifies, because its barrier waits on that same signal before Quiesce can
// succeed. A bounded edge's barrier promises only that its ring is idle, not
// that its goroutine has returned or stopped touching its scope -- it stays
// alive on purpose, to accept the delayed output Finalize's Flush may still
// push through the same queue -- so Host never opens a journal over it here.
// Its eventual downstream close is that goroutine's own act on its own Run
// journal, collected through the ordinary join once it does return.
func (e *Execution) Finish(ctx context.Context) ([]journal.Outcome, error) {
	if e == nil {
		return nil, ErrStarted
	}
	e.finishOnce.Do(func() {
		var failures []error
		for _, source := range e.sources {
			e.finished = append(e.finished, source.flush(ctx, &failures))
		}
		for index := len(e.edges) - 1; index >= 0; index-- {
			if edge := e.edges[index]; edge.chain.Valid() {
				e.finished = append(e.finished, edge.flush(ctx, &failures))
			}
		}
		e.finishErr = errors.Join(failures...)
	})
	return e.finished, e.finishErr
}

// flush runs one task's Finish under a fresh journal for the chain it drives.
// The failure it returns stays the caller's answer; what the flush could not
// release is the journal's. Only a namedTask whose chain is set -- a source or
// a join -- reaches here; see Finish for why a bounded edge must not.
func (n namedTask) flush(ctx context.Context, failures *[]error) journal.Outcome {
	flush := journal.New(journal.Flush, n.scope.Node())
	flush.Attach(n.scope.Identity(), n.name)
	n.chain.BindScope(flush)
	if err := n.task.Finish(ctx); err != nil {
		*failures = append(*failures, err)
	}
	return flush.Seal()
}

// Discard releases every owner still queued after data tasks have joined.
// Close must be called first on failure so producers can no longer publish.
//
// The tasks have joined and sealed their journals by now, so what is released
// here belongs to the caller's cleanup domain and is reported there.
func (e *Execution) Discard(into flow.Reporter) {
	if e == nil {
		return
	}
	for index := len(e.sources) - 1; index >= 0; index-- {
		e.sources[index].task.Discard(into)
	}
	for index := len(e.edges) - 1; index >= 0; index-- {
		e.edges[index].task.Discard(into)
	}
}

func (e *Execution) Close() {
	if e == nil {
		return
	}
	e.closeOnce.Do(func() {
		for index := len(e.sources) - 1; index >= 0; index-- {
			e.sources[index].task.Close()
		}
		for index := len(e.edges) - 1; index >= 0; index-- {
			e.edges[index].task.Close()
		}
	})
}

func (e *Execution) TaskCounts() (sources, edges int) {
	if e == nil {
		return 0, 0
	}
	return len(e.sources), len(e.edges)
}
