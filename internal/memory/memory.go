// Package memory grants coarse job resources to component instances. Payload
// allocation is accounted by each granted media/buffer allocator and never
// returns to this manager on the item path.
package memory

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/resource"
)

var (
	ErrClosed    = errors.New("resource grant manager is closed")
	ErrExhausted = errors.New("job resource grant is exhausted")
	ErrInvalid   = errors.New("resource reservation is invalid")
)

type Reservation struct {
	Name  string
	Grant resource.Grant
}

type Snapshot struct {
	Limit  resource.Grant
	Used   resource.Grant
	Active []Reservation
}

type Manager struct {
	mu     sync.Mutex
	limit  resource.Grant
	used   resource.Grant
	next   uint64
	active map[uint64]Reservation
	closed bool
}

func New(limit resource.Grant) *Manager {
	return &Manager{limit: limit, active: make(map[uint64]Reservation)}
}

// Reserve assigns exactly the component's compiled minimum. Runtime policy
// may construct the Manager with a larger job limit, but a component cannot
// discover or consume another component's slack.
func (m *Manager) Reserve(name string, request resource.Request) (*Lease, error) {
	if m == nil || name == "" || uint64(request.Memory) > math.MaxInt64 {
		return nil, ErrInvalid
	}
	grant := resource.Grant{
		Memory:    request.Memory,
		Temporary: request.Temporary,
		Workers:   request.Workers,
		Queue:     request.Queue,
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	if !fits(m.limit, m.used, grant) {
		return nil, fmt.Errorf("%w: %s", ErrExhausted, name)
	}
	var allocator *buffer.Allocator
	if grant.Memory != 0 {
		var err error
		allocator, err = buffer.NewAllocator(int64(grant.Memory))
		if err != nil {
			return nil, err
		}
	}
	id := m.next
	m.next++
	m.used = add(m.used, grant)
	m.active[id] = Reservation{Name: name, Grant: grant}
	return &Lease{manager: m, id: id, grant: grant, buffers: allocator}, nil
}

func (m *Manager) Close() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	m.mu.Lock()
	m.closed = true
	result := m.snapshotLocked()
	m.mu.Unlock()
	return result
}

func (m *Manager) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

func (m *Manager) snapshotLocked() Snapshot {
	result := Snapshot{Limit: m.limit, Used: m.used, Active: make([]Reservation, 0, len(m.active))}
	for _, reservation := range m.active {
		result.Active = append(result.Active, reservation)
	}
	sort.Slice(result.Active, func(left, right int) bool { return result.Active[left].Name < result.Active[right].Name })
	return result
}

type Lease struct {
	manager *Manager
	id      uint64
	grant   resource.Grant
	buffers *buffer.Allocator
	once    sync.Once
}

func (l *Lease) Grant() resource.Grant {
	if l == nil {
		return resource.Grant{}
	}
	return l.grant
}

func (l *Lease) Buffers() *buffer.Allocator {
	if l == nil {
		return nil
	}
	return l.buffers
}

func (l *Lease) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		if l.manager != nil {
			l.manager.release(l.id, l.grant)
		}
	})
	return nil
}

func (m *Manager) release(id uint64, grant resource.Grant) {
	m.mu.Lock()
	if _, exists := m.active[id]; exists {
		delete(m.active, id)
		m.used = subtract(m.used, grant)
	}
	m.mu.Unlock()
}

func fits(limit, used, requested resource.Grant) bool {
	return requested.Memory <= limit.Memory-used.Memory &&
		requested.Temporary <= limit.Temporary-used.Temporary &&
		requested.Workers <= limit.Workers-used.Workers &&
		requested.Queue <= limit.Queue-used.Queue
}

func add(left, right resource.Grant) resource.Grant {
	return resource.Grant{
		Memory:    left.Memory + right.Memory,
		Temporary: left.Temporary + right.Temporary,
		Workers:   left.Workers + right.Workers,
		Queue:     left.Queue + right.Queue,
	}
}

func subtract(left, right resource.Grant) resource.Grant {
	return resource.Grant{
		Memory:    left.Memory - right.Memory,
		Temporary: left.Temporary - right.Temporary,
		Workers:   left.Workers - right.Workers,
		Queue:     left.Queue - right.Queue,
	}
}
