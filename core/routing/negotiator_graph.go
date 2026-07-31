package routing

import (
	"fmt"
	"sort"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/registry"
)

// requiredPorts lists a filter's declared input ports, "in" first (so a
// port's ProfileRequirements can depend on "in" having already been
// resolved) and the rest in a stable, deterministic order.
func requiredPorts(m registry.TransformManifest) []string {
	ports := make([]string, 0, len(m.InputRequirements))
	hasIn := false
	for port := range m.InputRequirements {
		if port == "in" {
			hasIn = true
			continue
		}
		ports = append(ports, port)
	}
	sort.Strings(ports)
	if hasIn {
		ports = append([]string{"in"}, ports...)
	}
	return ports
}

// defaultBackboneSource is the implicit source for a filter's "in" port when
// nothing wires it explicitly. The backbone is the subsequence of filters
// whose own "in" port is not explicitly wired — a filter with an explicit
// "in" (e.g. one reading an auxiliary input) sits outside it entirely, the
// same way it used to be excluded from the old main filter chain, so it is
// skipped both as a consumer of the default and as a candidate predecessor.
// The source is the nearest such filter declared before index, or the main
// input if there is none. index may equal len(filters), meaning "after the
// last filter" (used to resolve the default output sink).
func defaultBackboneSource(index int, filters []FilterSpec) resolvedSource {
	for j := index - 1; j >= 0; j-- {
		if _, explicit := filters[j].Inputs["in"]; explicit {
			continue
		}
		return resolvedSource{nodeID: filterID("", j, filters[j].Alias), port: "out", filterIndex: j}
	}
	return resolvedSource{nodeID: "decoder", port: "out", filterIndex: -1}
}

// resolveGraphSource turns an explicit PortRef into a concrete node
// reference. Only aliased filters are addressable this way; an unaliased
// filter can still receive a port by declaration-order default, but nothing
// else can wire from its output.
func resolveGraphSource(ref PortRef, aliasIndex map[string]int, aux map[string]AuxInputSpec) (resolvedSource, error) {
	if ref.Alias == "" {
		return resolvedSource{}, fmt.Errorf("source alias must not be empty")
	}
	port := ref.Port
	if port == "" {
		port = "out"
	}
	if ref.Alias == MainInputAlias {
		return resolvedSource{nodeID: "decoder", port: port, filterIndex: -1}, nil
	}
	if _, ok := aux[ref.Alias]; ok {
		return resolvedSource{nodeID: fmt.Sprintf("aux:%s:decoder", ref.Alias), port: port, filterIndex: -1}, nil
	}
	if idx, ok := aliasIndex[ref.Alias]; ok {
		return resolvedSource{nodeID: "filter:" + ref.Alias, port: port, filterIndex: idx}, nil
	}
	return resolvedSource{}, fmt.Errorf("unknown source alias %q", ref.Alias)
}

// topologicalOrder returns filter indices in dependency order (a filter
// always comes after every other filter its inputs are wired from),
// preferring the lowest not-yet-ready index at each step so that a graph
// with no cross-order dependency reproduces plain declaration order.
func topologicalOrder(inDegree []int, dependents [][]int) ([]int, error) {
	remaining := append([]int(nil), inDegree...)
	done := make([]bool, len(inDegree))
	order := make([]int, 0, len(inDegree))
	for len(order) < len(inDegree) {
		next := -1
		for i, isDone := range done {
			if !isDone && remaining[i] == 0 {
				next = i
				break
			}
		}
		if next == -1 {
			return nil, fmt.Errorf("filter graph has a cycle")
		}
		done[next] = true
		order = append(order, next)
		for _, dependent := range dependents[next] {
			remaining[dependent]--
		}
	}
	return order, nil
}

func streamValues(set media.StreamSet) []media.StreamInfo {
	if len(set) == 0 {
		return nil
	}
	ports := make([]string, 0, len(set))
	for port := range set {
		ports = append(ports, port)
	}
	sort.Strings(ports)
	values := make([]media.StreamInfo, len(ports))
	for i, port := range ports {
		values[i] = set[port]
	}
	return values
}

func filterID(auxiliary string, index int, alias string) string {
	name := fmt.Sprintf("%d", index)
	if alias != "" {
		name = alias
	}
	if auxiliary == "" {
		return "filter:" + name
	}
	return fmt.Sprintf("aux:%s:filter:%s", auxiliary, name)
}
