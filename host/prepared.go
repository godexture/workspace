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
	preparedClosed
)

type reservation struct {
	name  string
	lease *memory.Lease
}

// Prepared is a one-shot executable Program with all coarse resources
// reserved. It does not open output transactions until Run.
type Prepared struct {
	program        program.Program
	manager        *memory.Manager
	reservations   []reservation
	byNode         map[job.NodeID]*memory.Lease
	direct         []bound.Entry
	observation    Observation
	cleanupTimeout time.Duration

	mu       sync.Mutex
	state    preparedState
	cancel   context.CancelCauseFunc
	done     chan struct{}
	doneOnce sync.Once
	closeErr error
	released sync.Once
}

// Prepare resolves the immutable Program and reserves every compiled node and
// runtime queue before any output or operator is opened.
func (h *Host) Prepare(ctx context.Context, request job.Job) (*Prepared, error) {
	selected, err := h.resolve(ctx, request)
	if err != nil {
		failure := Failure{Phase: PreparePhase, Err: errors.Join(err, closeRequestDirects(request))}
		return nil, &failure
	}
	entries := selected.Boundaries().Entries()
	failSelection := func(phase Phase, err error) (*Prepared, error) {
		failure := Failure{Phase: phase, Err: errors.Join(err, closeBoundDirects(entries))}
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
		observation:    h.observation,
		cleanupTimeout: h.cleanupTimeout,
		state:          preparedReady,
		done:           make(chan struct{}),
	}
	for _, entry := range entries {
		if entry.Projection().Kind == plan.DirectBoundary {
			prepared.direct = append(prepared.direct, entry)
		}
	}
	fail := func(err error) (*Prepared, error) {
		failure := Failure{Phase: ResourcePhase, Err: errors.Join(err, joinFailures(prepared.releaseReservations()))}
		return nil, &failure
	}
	runtimeRequest, err := selected.RuntimeResources()
	if err != nil {
		return fail(err)
	}
	if runtimeRequest != (resource.Request{}) {
		lease, err := manager.Reserve("runtime", runtimeRequest)
		if err != nil {
			return fail(err)
		}
		prepared.reservations = append(prepared.reservations, reservation{name: "runtime", lease: lease})
	}
	for _, node := range selected.Nodes() {
		lease, err := manager.Reserve(node.ID().String(), node.Compilation().Resources())
		if err != nil {
			return fail(err)
		}
		prepared.reservations = append(prepared.reservations, reservation{name: node.ID().String(), lease: lease})
		prepared.byNode[node.ID()] = lease
	}
	return prepared, nil
}

func grantOf(request resource.Request) resource.Grant {
	return resource.Grant{
		Memory:    request.Memory,
		Temporary: request.Temporary,
		Workers:   request.Workers,
		Queue:     request.Queue,
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
		p.state = preparedClosed
		p.mu.Unlock()
		failures := p.releaseReservations()
		err := joinFailures(failures)
		p.mu.Lock()
		p.closeErr = err
		p.mu.Unlock()
		p.closeDone()
		return err
	case preparedRunning:
		cancel := p.cancel
		done := p.done
		timeout := p.cleanupTimeout
		p.mu.Unlock()
		if cancel != nil {
			cancel(context.Canceled)
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
	case preparedClosed:
		err := p.closeErr
		p.mu.Unlock()
		return err
	default:
		p.mu.Unlock()
		return errors.New("prepared job is invalid")
	}
}

func (p *Prepared) releaseReservations() (failures []Failure) {
	p.released.Do(func() {
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
			if err := entry.Close(); err != nil {
				failures = append(failures, Failure{Phase: ClosePhase, Node: entry.Projection().Node, Err: err})
			}
		}
	})
	return failures
}

func closeRequestDirects(request job.Job) error {
	var failures []error
	for _, input := range request.Inputs() {
		if direct, ok := input.Direct(); ok {
			failures = append(failures, direct.Close())
		}
	}
	for _, output := range request.Outputs() {
		if direct, ok := output.Direct(); ok {
			failures = append(failures, direct.Close())
		}
	}
	return errors.Join(failures...)
}

func closeBoundDirects(entries []bound.Entry) error {
	var failures []error
	for index := len(entries) - 1; index >= 0; index-- {
		if entries[index].Projection().Kind == plan.DirectBoundary {
			failures = append(failures, entries[index].Close())
		}
	}
	return errors.Join(failures...)
}

func (p *Prepared) complete(err error) {
	p.mu.Lock()
	p.state = preparedClosed
	p.closeErr = err
	p.cancel = nil
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
func (h *Host) Run(ctx context.Context, request job.Job) (Result, error) {
	prepared, err := h.Prepare(ctx, request)
	if err != nil {
		failure := failureOf(PreparePhase, "", "", err)
		return Result{Primary: &failure}, err
	}
	result, runErr := prepared.Run(ctx)
	if closeErr := prepared.Close(); runErr == nil && closeErr != nil {
		runErr = closeErr
	}
	return result, runErr
}
