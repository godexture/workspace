package routing

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/godexture/godec/core/node"
)

func appendPlanNodes(owned []node.Node, plans []transformPlan) []node.Node {
	for _, plan := range plans {
		owned = append(owned, plan.node)
	}
	return owned
}

func releaseOwnedNode(owned []node.Node, target node.Node) []node.Node {
	for i, current := range owned {
		if sameNode(current, target) {
			return append(owned[:i], owned[i+1:]...)
		}
	}
	return owned
}

func sameNode(first, second node.Node) bool {
	if first == nil || second == nil {
		return first == second
	}
	firstValue := reflect.ValueOf(first)
	secondValue := reflect.ValueOf(second)
	if firstValue.Type() != secondValue.Type() || !firstValue.Type().Comparable() {
		return false
	}
	return firstValue.Interface() == secondValue.Interface()
}

func closeOwnedNodes(nodes []node.Node) error {
	var result error
	for i := len(nodes) - 1; i >= 0; i-- {
		if nodes[i] == nil {
			continue
		}
		if err := nodes[i].Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("close unowned node %d (%T): %w", i, nodes[i], err))
		}
	}
	return result
}
