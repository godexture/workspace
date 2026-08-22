package run

import (
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/run/drive"
)

func (t Template) outputLink(ledger *journal.Ledger, index int, targets []drive.Link, execution *Execution) (drive.Link, error) {
	node := t.nodes[index].id.String()
	links, err := t.outputLinks(ledger, index, targets, execution)
	if err != nil {
		return drive.Link{}, err
	}
	return t.nodes[index].binding.Fanout(links, node)
}

func (t Template) routeLinks(ledger *journal.Ledger, index int, targets []drive.Link, execution *Execution) ([]drive.Link, error) {
	links, err := t.outputLinks(ledger, index, targets, execution)
	if err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return nil, ErrTopology
	}
	maxRoute := -1
	for _, connectionIndex := range t.outgoing[index] {
		if route := t.connections[connectionIndex].route; route > maxRoute {
			maxRoute = route
		}
	}
	routes := make([][]drive.Link, maxRoute+1)
	for outputIndex, connectionIndex := range t.outgoing[index] {
		route := t.connections[connectionIndex].route
		if route < 0 || route >= len(routes) {
			return nil, ErrTopology
		}
		routes[route] = append(routes[route], links[outputIndex])
	}
	result := make([]drive.Link, len(routes))
	node := t.nodes[index].id.String()
	for route, outputs := range routes {
		if len(outputs) == 0 {
			return nil, ErrTopology
		}
		result[route], err = t.nodes[index].binding.Fanout(outputs, node)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (t Template) outputLinks(ledger *journal.Ledger, index int, targets []drive.Link, execution *Execution) ([]drive.Link, error) {
	links := make([]drive.Link, len(t.outgoing[index]))
	for outputIndex, connectionIndex := range t.outgoing[index] {
		connection := t.connections[connectionIndex]
		link := targets[connectionIndex]
		if !link.Valid() {
			return nil, ErrTopology
		}
		key := connectionKey(t.edges[connection.logical].value, connection.route, connection.input)
		local := execution.observer.Local("", key)
		observed, err := t.nodes[index].binding.Observe(link, local)
		if err != nil {
			return nil, err
		}
		link = observed
		zipInput := t.nodes[connection.to].kind == drive.Joiner && t.nodes[connection.to].binding.FanIn() == flow.ZipFanIn
		if connection.reason != 0 && !zipInput {
			buffered, bufferTask, err := t.nodes[index].binding.Buffer(connection.limit, link, ledger.Domain("buffer/"+key, key))
			if err != nil {
				return nil, err
			}
			link = buffered
			execution.edges = append(execution.edges, namedTask{task: bufferTask})
		}
		links[outputIndex] = link
	}
	return links, nil
}
