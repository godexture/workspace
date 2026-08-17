package run

import (
	"errors"
	"math"

	"github.com/godexture/godec/job"
)

// QueueSlots reports the exact slots reserved by private physical queues.
// Public Plan buffers are logical-edge projections and must not be used for
// resource accounting.
func (t Template) QueueSlots() (uint32, error) {
	var slots uint64
	for _, connection := range t.connections {
		if connection.reason == 0 {
			continue
		}
		if connection.limit.Items < 0 || uint64(connection.limit.Items) > math.MaxUint32-slots {
			return 0, errors.New("runtime queue resource request overflows")
		}
		slots += uint64(connection.limit.Items)
	}
	return uint32(slots), nil
}

// InFlightMultiplier returns the maximum retained copies of an item produced
// by node: every reachable operator holder and every reachable physical queue
// slot.
func (t Template) InFlightMultiplier(id job.NodeID) (uint64, error) {
	start := -1
	for index, node := range t.nodes {
		if node.id == id {
			start = index
			break
		}
	}
	if start < 0 {
		return 0, errors.New("runtime node is absent")
	}
	reachable := make([]bool, len(t.nodes))
	pending := []int{start}
	for len(pending) != 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if reachable[current] {
			continue
		}
		reachable[current] = true
		for _, connectionIndex := range t.outgoing[current] {
			pending = append(pending, t.connections[connectionIndex].to)
		}
	}
	var multiplier uint64
	for _, value := range reachable {
		if value {
			multiplier++
		}
	}
	for _, connection := range t.connections {
		if connection.reason == 0 || !reachable[connection.from] {
			continue
		}
		if connection.limit.Items <= 0 || uint64(connection.limit.Items) > math.MaxUint64-multiplier {
			return 0, errors.New("runtime payload in-flight bound overflows")
		}
		multiplier += uint64(connection.limit.Items)
	}
	return multiplier, nil
}
