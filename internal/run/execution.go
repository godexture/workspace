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
	ErrUnboundChain  = errors.New("runtime delivery chain has no failure domain")
)

type namedTask struct {
	task  drive.Task
	chain drive.Link
	done  chan error
}

func (n namedTask) domain() *journal.Domain { return n.task.Domain() }

// Execution is a one-shot opened data path. It owns only runtime edge state;
// Host retains operator/resource lifecycle ownership.
type Execution struct {
	edges   []namedTask
	sources []namedTask

	mu         sync.Mutex
	started    bool
	abortOnce  sync.Once
	finishOnce sync.Once
	finishErr  error
	observer   *observe.Collector
}

func (t Template) Build(ledger *journal.Ledger, operators []flow.Operator) (*Execution, error) {
	return t.BuildObserved(ledger, operators, nil)
}

func (t Template) BuildObserved(ledger *journal.Ledger, operators []flow.Operator, observer *observe.Collector) (*Execution, error) {
	if ledger == nil {
		return nil, ErrUnboundChain
	}
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
	edgeTargets := make([]drive.Link, len(t.connections))
	fail := func(err error) (*Execution, error) {
		result.Abort()
		return nil, err
	}

	for index := len(t.nodes) - 1; index >= 0; index-- {
		value := t.nodes[index]
		operator := operators[index]
		node := value.id.String()
		switch value.kind {
		case drive.Sink:
			link, err := value.binding.OpenSinkAt(operator, node)
			if err != nil {
				return fail(err)
			}
			edgeTargets[t.incoming[index][0]] = link
		case drive.Processor:
			output, err := t.outputLink(ledger, index, edgeTargets, result)
			if err != nil {
				return fail(err)
			}
			input, err := value.binding.PrependAt(operator, output, node)
			if err != nil {
				return fail(err)
			}
			edgeTargets[t.incoming[index][0]] = input
		case drive.Router:
			routes, err := t.routeLinks(ledger, index, edgeTargets, result)
			if err != nil {
				return fail(err)
			}
			input, err := value.binding.OpenRouterAt(operator, routes, node)
			if err != nil {
				return fail(err)
			}
			edgeTargets[t.incoming[index][0]] = input
		case drive.Joiner:
			output, err := t.outputLink(ledger, index, edgeTargets, result)
			if err != nil {
				return fail(err)
			}
			incoming := append([]int(nil), t.incoming[index]...)
			sort.Slice(incoming, func(left, right int) bool {
				return t.connections[incoming[left]].input < t.connections[incoming[right]].input
			})
			joinInputs := make([]drive.JoinInput, len(incoming))
			for inputIndex, edgeIndex := range incoming {
				connection := t.connections[edgeIndex]
				joinInputs[inputIndex] = drive.JoinInput{Limit: connection.limit, Base: connection.descriptor.TimeBase()}
			}
			inputs, joinTask, err := value.binding.OpenJoiner(operator, joinInputs, value.toleranceTicks, output, ledger.Domain("join/"+node, node))
			if err != nil {
				return fail(err)
			}
			if len(inputs) != len(incoming) {
				return fail(ErrTopology)
			}
			for inputIndex, edgeIndex := range incoming {
				edgeTargets[edgeIndex] = inputs[inputIndex]
			}
			result.edges = append(result.edges, namedTask{task: joinTask, chain: output})
		case drive.Source:
			output, err := t.outputLink(ledger, index, edgeTargets, result)
			if err != nil {
				return fail(err)
			}
			sourceTask, err := value.binding.OpenSource(operator, output, ledger.Domain("source/"+node, node))
			if err != nil {
				return fail(err)
			}
			result.sources = append(result.sources, namedTask{task: sourceTask, chain: output, done: make(chan error, 1)})
		default:
			return fail(ErrTopology)
		}
	}
	for _, link := range edgeTargets {
		if !link.Valid() {
			return fail(ErrTopology)
		}
		// Every chain is bound by the constructor of the task that drives it,
		// so this can only fail if a new edge kind forgot to have an owner at
		// all. Finding that here costs one pass at Open; finding it later
		// means a release failure with nowhere to go.
		if !link.Bound() {
			return fail(ErrUnboundChain)
		}
	}
	return result, nil
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
		if err := group.StartDomain(value.domain(), value.task.Run, value.task.Sealed()); err != nil {
			group.Cancel(err)
			e.Abort()
			return err
		}
	}
	for _, value := range e.sources {
		current := value
		// current.done must not be filled from inside work: the Run span
		// records what work returned only after it returns, so a signal sent
		// from in there would race the Flush span Finish opens on the same
		// domain once WaitSources wakes up. The sealed hook runs after the
		// span has ended.
		sealed := func(cause error) { current.done <- cause }
		if err := group.StartDomain(current.domain(), current.task.Run, sealed); err != nil {
			group.Cancel(err)
			e.Abort()
			return err
		}
	}
	return nil
}

// WaitSources waits for Reader EOF without sealing the shared task group;
// edge tasks remain live while Host performs Finalize.
//
// What it returns on a source failure is that source's cause, which is a
// reference to the ledger event already recording it. Nothing here produces a
// second description of the same failure.
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
			// The cause, never Err: Err flattens whatever stopped the run into
			// a bare cancellation, and a bare cancellation is a new failure
			// wherever it is recorded rather than the one that already
			// happened.
			return context.Cause(ctx)
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
// Flushing is its own lifecycle operation, so it gets its own span -- opened
// on the same stable domain the task ran under, never a second domain. That
// matters because an ownership slot filled during Run keeps the site it was
// bound to, and the contract explicitly allows a component to still be holding
// one: a collector or transport retaining a cell across calls releases it
// here. A second domain would leave such a cell reporting somewhere this run
// no longer looks, while the domain it was bound to stays the domain the
// ledger collects from for the whole run.
//
// Reaching across goroutines to open that span is only safe for a chain no
// other goroutine can still be touching. A source qualifies, because
// WaitSources already observed the signal its sealed hook sends after its Run
// span ended; a join qualifies, because its barrier waits on the same signal
// before Quiesce can succeed. A bounded edge does not: its barrier promises
// only that its ring is idle, and it stays alive on purpose to accept the
// delayed output Finalize's Flush may still push through the same queue. Its
// own downstream close is therefore its own act, in a Flush span it opens on
// its own domain when it sees EOF.
//
// What Finish returns is the cause to stop on, or nil. Every failure it
// produced is already in the ledger, so this is a signal rather than a second
// report.
func (e *Execution) Finish(ctx context.Context) error {
	if e == nil {
		return nil
	}
	e.finishOnce.Do(func() {
		for _, source := range e.sources {
			e.finishErr = firstCause(e.finishErr, source.flush(ctx))
		}
		for index := len(e.edges) - 1; index >= 0; index-- {
			if edge := e.edges[index]; edge.chain.Valid() {
				e.finishErr = firstCause(e.finishErr, edge.flush(ctx))
			}
		}
	})
	return e.finishErr
}

func firstCause(existing, next error) error {
	if existing != nil {
		return existing
	}
	return next
}

// flush performs one task's Finish as its own Flush span on the task's own
// domain, through the same boundary a task's goroutine uses for Run, so a
// panic during this run-driven hand-off cannot discard what it recorded any
// more than a task's own panic can. Only a namedTask whose chain is set
// reaches here; see Finish for why.
func (n namedTask) flush(ctx context.Context) error {
	return n.domain().Perform(journal.Flush, func(*journal.Span) error {
		return n.task.Finish(ctx)
	})
}

// Discard releases every owner still queued after data tasks have joined.
// Abort must be called first on failure so producers can no longer publish.
//
// What is released still belongs to the task that owned the edge, so it is
// released in that task's domain rather than handed to a cleanup domain of the
// caller's. The Discard span is what places it in the run's lifecycle, and it
// also recovers a declared Drop that panics past every other owner.
func (e *Execution) Discard() {
	if e == nil {
		return
	}
	for index := len(e.sources) - 1; index >= 0; index-- {
		e.sources[index].discard()
	}
	for index := len(e.edges) - 1; index >= 0; index-- {
		e.edges[index].discard()
	}
}

func (n namedTask) discard() {
	n.domain().Perform(journal.Discard, func(*journal.Span) error {
		n.task.Discard()
		return nil
	})
}

// Abort stops runtime edges without declaring ordinary end-of-stream. It is
// idempotent and must follow cancellation, so a task waking from an aborted
// queue returns that cancellation cause rather than producing a synthetic EOF.
func (e *Execution) Abort() {
	if e == nil {
		return
	}
	e.abortOnce.Do(func() {
		for index := len(e.sources) - 1; index >= 0; index-- {
			e.sources[index].task.Abort()
		}
		for index := len(e.edges) - 1; index >= 0; index-- {
			e.edges[index].task.Abort()
		}
	})
}

func (e *Execution) TaskCounts() (sources, edges int) {
	if e == nil {
		return 0, 0
	}
	return len(e.sources), len(e.edges)
}
