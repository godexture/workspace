package program

import (
	"errors"
	"math"

	"github.com/godexture/godec/job"
	"github.com/godexture/godec/resource"
)

// Resources returns the exact coarse reservation required by compiled nodes
// and runtime queue slots. Node payload memory includes every downstream
// queue slot that can retain an item produced from that node.
func (p Program) Resources() (resource.Request, error) {
	if !p.Valid() {
		return resource.Request{}, errors.New("program is invalid")
	}
	var result resource.Request
	for _, node := range p.nodes {
		request, err := p.NodeResources(node.ID())
		if err != nil {
			return resource.Request{}, err
		}
		if err := addRequest(&result, request); err != nil {
			return resource.Request{}, err
		}
	}
	runtime, err := p.RuntimeResources()
	if err != nil {
		return resource.Request{}, err
	}
	if err := addRequest(&result, runtime); err != nil {
		return resource.Request{}, err
	}
	return result, nil
}

// NodeResources expands one component's per-item payload request by the
// maximum number of places that can retain its storage at once: every
// downstream queue slot, plus the item each reachable operator holds while it
// works.
func (p Program) NodeResources(id job.NodeID) (resource.Request, error) {
	if !p.Valid() {
		return resource.Request{}, errors.New("program is invalid")
	}
	node, ok := p.Lookup(id)
	if !ok {
		return resource.Request{}, errors.New("program node is absent")
	}
	request := node.Compilation().Resources()
	multiplier, err := p.runtime.InFlightMultiplier(id)
	if err != nil {
		return resource.Request{}, err
	}
	request.Memory, err = scaleMemory(request.Memory, multiplier)
	if err != nil {
		return resource.Request{}, err
	}
	return request, nil
}

func (p Program) RuntimeResources() (resource.Request, error) {
	if !p.Valid() {
		return resource.Request{}, errors.New("program is invalid")
	}
	slots, err := p.runtime.QueueSlots()
	if err != nil {
		return resource.Request{}, err
	}
	return resource.Request{Queue: slots}, nil
}

func scaleMemory(memory resource.Bytes, multiplier uint64) (resource.Bytes, error) {
	if memory == 0 {
		return 0, nil
	}
	if multiplier == 0 || uint64(memory) > math.MaxUint64/multiplier {
		return 0, errors.New("program payload memory request overflows")
	}
	return resource.Bytes(uint64(memory) * multiplier), nil
}

func addRequest(total *resource.Request, value resource.Request) error {
	if uint64(total.Memory) > math.MaxUint64-uint64(value.Memory) ||
		uint64(total.Workers)+uint64(value.Workers) > math.MaxUint32 ||
		uint64(total.Queue)+uint64(value.Queue) > math.MaxUint32 {
		return errors.New("program resource request overflows")
	}
	total.Memory += value.Memory
	total.Workers += value.Workers
	total.Queue += value.Queue
	return nil
}
