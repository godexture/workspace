package run

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/godexture/godec/flow"
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
	scope *drive.Scope
}

// Execution is a one-shot opened data path. It owns only runtime edge state;
// Host retains operator/resource lifecycle ownership.
type Execution struct {
	edges   []namedTask
	sources []namedTask

	mu        sync.Mutex
	started   bool
	closeOnce sync.Once
	observer  *observe.Collector
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
			scope := drive.NewScope(value.id.String())
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
			result.edges = append(result.edges, namedTask{name: "join/" + value.id.String(), task: joinTask, scope: scope})
		case drive.Source:
			output, err := t.outputLink(index, edgeTargets, result)
			if err != nil {
				return fail(err)
			}
			scope := drive.NewScope(value.id.String())
			output.BindScope(scope)
			sourceTask, err := value.binding.OpenSource(operator, output)
			if err != nil {
				return fail(err)
			}
			result.sources = append(result.sources, namedTask{name: "source/" + value.id.String(), task: sourceTask, scope: scope})
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
			scope := drive.NewScope("")
			link.BindScope(scope)
			buffered, bufferTask, err := t.nodes[index].binding.Buffer(edge.limit, link)
			if err != nil {
				return drive.Link{}, err
			}
			link = buffered
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
		if err := group.StartScoped(value.name, value.scope.Node, value.scope.Cleanup, value.task.Run); err != nil {
			e.Close()
			group.Cancel(err)
			return err
		}
	}
	for _, value := range e.sources {
		if err := group.StartScoped(value.name, value.scope.Node, value.scope.Cleanup, value.task.Run); err != nil {
			e.Close()
			group.Cancel(err)
			return err
		}
	}
	return nil
}

func (e *Execution) Run(ctx context.Context) task.Report {
	group := task.New(ctx)
	if err := e.Start(group); err != nil {
		return task.Report{Failures: []task.Failure{{Name: "runtime/start", Err: err}}}
	}
	return group.Wait(context.Background())
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
