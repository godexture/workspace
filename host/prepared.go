package host

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/internal/memory"
	"github.com/godexture/godec/internal/program"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/resource"
)

type preparedState uint8

const (
	preparedReady preparedState = iota + 1
	preparedRunning
	preparedClosing
	preparedClosed
)

type reservation struct {
	name  string
	lease *memory.Lease
}

// Prepared is a one-shot executable Program with coarse resources and Access
// sessions owned for its lifetime. Output state remains private until Run
// commits it.
type Prepared struct {
	program        program.Program
	manager        *memory.Manager
	reservations   []reservation
	byNode         map[job.NodeID]*memory.Lease
	sessions       []acquiredSession
	probeStores    []*probeStore
	bySession      map[string]acquiredSession
	sources        formatSources
	direct         []bound.Entry
	scratch        map[job.NodeID]scratchLease
	temporary      map[job.NodeID]scratchLease
	cleanupTimeout time.Duration

	mu              sync.Mutex
	state           preparedState
	stop            func(error)
	done            chan struct{}
	doneOnce        sync.Once
	closeErr        error
	released        sync.Once
	releaseFailures []Failure
	scratchReleased sync.Once
	scratchFailures []Failure
}

// Prepare acquires and inspects inputs, resolves the immutable Program,
// reserves every compiled node and runtime queue, and only then acquires
// output sessions before any operator opens.
func (h *Host) Prepare(ctx context.Context, request job.Job) (*Prepared, error) {
	planning, err := h.resolveInputs(ctx, request)
	if err != nil {
		failure := failureOf(PreparePhase, "", "", err)
		return nil, &failure
	}
	selected := planning.program
	entries := planning.entries
	failSelection := func(phase Phase, err error) (*Prepared, error) {
		failure := Failure{Phase: phase, Err: errors.Join(err, h.closeInputPlan(planning))}
		return nil, &failure
	}
	if !selected.Executable() {
		return failSelection(PreparePhase, errors.New("selected Plan has no complete typed execution binding"))
	}
	total, err := selected.Resources()
	if err != nil {
		return failSelection(ResourcePhase, err)
	}
	limit := grantOf(total)
	policy := selected.Plan().EffectivePolicy().Resources
	if policy.Limited {
		limit = policy.Limit
		if !limit.Satisfies(total) {
			return failSelection(ResourcePhase, fmt.Errorf("job resource limit does not satisfy compiled request"))
		}
	}
	manager := memory.New(limit)
	prepared := &Prepared{
		program:        selected,
		manager:        manager,
		byNode:         make(map[job.NodeID]*memory.Lease),
		bySession:      make(map[string]acquiredSession),
		scratch:        make(map[job.NodeID]scratchLease),
		temporary:      make(map[job.NodeID]scratchLease),
		sources:        make(formatSources, len(planning.sources)),
		sessions:       append([]acquiredSession(nil), planning.sessions...),
		probeStores:    append([]*probeStore(nil), planning.stores...),
		cleanupTimeout: h.cleanupTimeout,
		state:          preparedReady,
		done:           make(chan struct{}),
	}
	for node, source := range planning.sources {
		prepared.sources[node] = source
	}
	for _, entry := range entries {
		if entry.Projection().Kind == plan.DirectBoundary {
			prepared.direct = append(prepared.direct, entry)
		}
	}
	for _, session := range prepared.sessions {
		prepared.bySession[session.node] = session
	}
	fail := func(phase Phase, err error) (*Prepared, error) {
		cleanupContext, cancel := context.WithTimeout(context.Background(), h.cleanupTimeout)
		defer cancel()
		failure := failureOf(phase, "", "", err)
		failure.Err = errors.Join(failure.Err, joinFailures(prepared.releaseScratch()), joinFailures(prepared.releaseResources(cleanupContext)))
		return nil, &failure
	}
	runtimeRequest, err := selected.RuntimeResources()
	if err != nil {
		return fail(ResourcePhase, err)
	}
	if runtimeRequest != (resource.Request{}) {
		lease, err := manager.Reserve("runtime", runtimeRequest)
		if err != nil {
			return fail(ResourcePhase, err)
		}
		prepared.reservations = append(prepared.reservations, reservation{name: "runtime", lease: lease})
	}
	for _, node := range selected.Nodes() {
		request, err := selected.NodeResources(node.ID())
		if err != nil {
			return fail(ResourcePhase, err)
		}
		lease, err := manager.Reserve(node.ID().String(), request)
		if err != nil {
			return fail(ResourcePhase, err)
		}
		prepared.reservations = append(prepared.reservations, reservation{name: node.ID().String(), lease: lease})
		prepared.byNode[node.ID()] = lease
	}
	prepared.scratch, err = openScratch(selected)
	if err != nil {
		return fail(ResourcePhase, err)
	}
	prepared.temporary, err = openTemporary(selected)
	if err != nil {
		return fail(ResourcePhase, err)
	}
	outputSessions, acquireErr := acquireSessions(ctx, entries, plan.OutputBoundary)
	prepared.sessions = append(prepared.sessions, outputSessions...)
	for _, session := range outputSessions {
		prepared.bySession[session.node] = session
	}
	if acquireErr != nil {
		return fail(PreparePhase, acquireErr)
	}
	return prepared, nil
}

func grantOf(request resource.Request) resource.Grant {
	return resource.Grant{
		Memory:  request.Memory,
		Workers: request.Workers,
		Queue:   request.Queue,
	}
}

// Plan returns the immutable public projection selected during Prepare.
func (p *Prepared) Plan() plan.Plan {
	if p == nil {
		return plan.Plan{}
	}
	return p.program.Plan()
}

// Close is idempotent. When Run is active it requests cancellation and waits
// only for the configured cleanup bound.
func (p *Prepared) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	switch p.state {
	case preparedReady:
		p.state = preparedClosing
		p.mu.Unlock()
		cleanupContext, cancel := context.WithTimeout(context.Background(), p.cleanupTimeout)
		failures := append(p.releaseScratch(), p.releaseResources(cleanupContext)...)
		cancel()
		err := joinFailures(failures)
		p.complete(err)
		return err
	case preparedRunning:
		stop := p.stop
		done := p.done
		timeout := p.cleanupTimeout
		p.mu.Unlock()
		if stop != nil {
			stop(context.Canceled)
		}
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-done:
			p.mu.Lock()
			err := p.closeErr
			p.mu.Unlock()
			return err
		case <-timer.C:
			return fmt.Errorf("prepared job cleanup exceeded %s", timeout)
		}
	case preparedClosing:
		done := p.done
		p.mu.Unlock()
		<-done
		p.mu.Lock()
		err := p.closeErr
		p.mu.Unlock()
		return err
	case preparedClosed:
		err := p.closeErr
		p.mu.Unlock()
		return err
	default:
		p.mu.Unlock()
		return errors.New("prepared job is invalid")
	}
}

func (p *Prepared) releaseResources(ctx context.Context) []Failure {
	p.released.Do(func() {
		var failures []Failure
		failures = append(failures, closeSessions(ctx, p.sessions)...)
		if err := closeProbeStores(p.probeStores); err != nil {
			failures = append(failures, Failure{Phase: ResourcePhase, Err: err})
		}
		for index := len(p.reservations) - 1; index >= 0; index-- {
			value := p.reservations[index]
			if allocator := value.lease.Buffers(); allocator != nil && allocator.Used() != 0 {
				failures = append(failures, Failure{Phase: ResourcePhase, Node: value.name, Err: fmt.Errorf("payload allocator retained %d bytes", allocator.Used())})
			}
			if err := value.lease.Close(); err != nil {
				failures = append(failures, Failure{Phase: ResourcePhase, Node: value.name, Err: err})
			}
		}
		snapshot := p.manager.Close()
		if len(snapshot.Active) != 0 {
			failures = append(failures, Failure{Phase: ResourcePhase, Err: fmt.Errorf("resource manager retained %d reservations", len(snapshot.Active))})
		}
		for index := len(p.direct) - 1; index >= 0; index-- {
			entry := p.direct[index]
			if err := protectedCall(entry.Projection().Node, "direct/close", func() error { return entry.Close() }); err != nil {
				failures = append(failures, failureOf(ClosePhase, entry.Projection().Node, "direct/close", err))
			}
		}
		p.releaseFailures = failures
	})
	result := make([]Failure, len(p.releaseFailures))
	copy(result, p.releaseFailures)
	for index := range result {
		result[index].Stack = append([]byte(nil), result[index].Stack...)
	}
	return result
}

func closeRequestDirects(request job.Job) error {
	var failures []error
	for _, input := range request.Inputs() {
		if direct, ok := input.Direct(); ok {
			if err := protectedCall("", "direct/close", func() error { return direct.Close() }); err != nil {
				failures = append(failures, err)
			}
		}
	}
	for _, output := range request.Outputs() {
		if direct, ok := output.Direct(); ok {
			if err := protectedCall("", "direct/close", func() error { return direct.Close() }); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}

func closeBoundDirects(entries []bound.Entry) error {
	var failures []error
	for index := len(entries) - 1; index >= 0; index-- {
		if entries[index].Projection().Kind == plan.DirectBoundary {
			if err := protectedCall(entries[index].Projection().Node, "direct/close", func() error { return entries[index].Close() }); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}

func (p *Prepared) complete(err error) {
	p.mu.Lock()
	p.state = preparedClosed
	p.closeErr = err
	p.stop = nil
	p.mu.Unlock()
	p.closeDone()
}

func (p *Prepared) closeDone() { p.doneOnce.Do(func() { close(p.done) }) }

func joinFailures(values []Failure) error {
	errorsList := make([]error, len(values))
	for index := range values {
		errorsList[index] = values[index]
	}
	return errors.Join(errorsList...)
}

// Run is the convenience form for Prepare followed by one execution.
func (h *Host) Run(ctx context.Context, request job.Job, options ...RunOption) (Result, error) {
	configuration, err := resolveRunOptions(options)
	if err != nil {
		failure := failureOf(RunPhase, "", "", err)
		return Result{Primary: &failure}, &failure
	}
	prepared, err := h.Prepare(ctx, request)
	if err != nil {
		failure := failureOf(PreparePhase, "", "", err)
		return Result{Primary: &failure}, err
	}
	result, runErr := prepared.run(ctx, configuration)
	if closeErr := prepared.Close(); runErr == nil && closeErr != nil {
		runErr = closeErr
	}
	return result, runErr
}
