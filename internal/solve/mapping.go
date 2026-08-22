package solve

import (
	"strconv"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/graph"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

func (p *planner) projectMappings(compiled graph.Graph) ([]plan.Mapping, error) {
	requested := p.request.Mappings()
	readerID, readerSelected := p.formats[formatBoundary{direction: plan.InputBoundary, choice: 0}]
	writerID, writerSelected := p.formats[formatBoundary{direction: plan.OutputBoundary, choice: 0}]
	if !readerSelected || !writerSelected {
		if len(requested) == 0 {
			return nil, nil
		}
		return nil, mappingError("solve.mapping-boundary", "exact stream mapping has no resolved Format boundary", plugin.Identity{}, map[string]string{
			"input": strconv.FormatBool(readerSelected), "output": strconv.FormatBool(writerSelected),
		})
	}
	reader, readerOK := compiled.Lookup(readerID)
	writer, writerOK := compiled.Lookup(writerID)
	if !readerOK || !writerOK {
		return nil, mappingError("solve.mapping-node", "resolved Format node is absent from the compiled graph", plugin.Identity{}, map[string]string{
			"reader": readerID.String(), "writer": writerID.String(),
		})
	}
	readerComponent, readerOK := p.index.Lookup(reader.Component())
	writerComponent, writerOK := p.index.Lookup(writer.Component())
	if !readerOK || !writerOK {
		return nil, mappingError("solve.mapping-component", "resolved Format component disappeared from the catalog", plugin.Identity{}, nil)
	}
	read, readOK := mediaformat.ReadOf(readerComponent)
	write, writeOK := mediaformat.WriteOf(writerComponent)
	readerPort, readerMany := onlyManyPort(reader.Shape(), flow.OutputDirection)
	writerPort, writerMany := onlyManyPort(writer.Shape(), flow.InputDirection)
	if !readOK || !read.Valid() || !writeOK || !write.Valid() || !readerMany || !writerMany {
		if len(requested) == 0 {
			return nil, nil
		}
		return nil, mappingError("solve.mapping-shape", "exact stream mapping requires a Many-output reader and a Many-input writer", readerComponent.Identity(), map[string]string{
			"reader": readerID.String(), "writer": writerID.String(),
		})
	}

	readerStreams, err := mappingStreams(reader.Outputs().At(readerPort))
	if err != nil {
		return nil, mappingError("solve.mapping-reader", "input Format compiled an invalid stream mapping", readerComponent.Identity(), map[string]string{"cause": err.Error()})
	}
	if len(readerStreams) == 0 {
		return nil, mappingError("solve.mapping-reader", "input Format compiled no mapped streams", readerComponent.Identity(), nil)
	}
	if len(requested) != 0 {
		selection, ok := mediaformat.SelectionOf(p.contexts.For(readerID), read.Format())
		if !ok {
			return nil, mappingError("solve.mapping-selection", "input Format has no exact stream selection", readerComponent.Identity(), nil)
		}
		if !sameStreamSet(readerStreams, selection.Streams()) {
			return nil, mappingError("solve.mapping-selection", "input Format did not compile exactly the selected stream set", readerComponent.Identity(), map[string]string{
				"compiled": strconv.Itoa(len(readerStreams)), "selected": strconv.Itoa(len(selection.Streams())),
			})
		}
		requestedStreams := make([]stream.ID, len(requested))
		for index, mapping := range requested {
			requestedStreams[index] = mapping.Stream()
		}
		if !sameStreamSet(selection.Streams(), requestedStreams) {
			return nil, mappingError("solve.mapping-selection", "prepared stream selection does not match the Job mapping", readerComponent.Identity(), nil)
		}
	}

	writerStreams, err := mappingStreams(writer.Inputs().At(writerPort))
	if err != nil {
		return nil, mappingError("solve.mapping-writer", "output Format received an invalid stream mapping", writerComponent.Identity(), map[string]string{"cause": err.Error()})
	}
	if !sameStreamOrder(readerStreams, writerStreams) {
		return nil, mappingError("solve.mapping-writer", "output Format inputs do not match input Format outputs in order", writerComponent.Identity(), map[string]string{
			"reader": strconv.Itoa(len(readerStreams)), "writer": strconv.Itoa(len(writerStreams)),
		})
	}
	result := make([]plan.Mapping, len(readerStreams))
	for index, id := range readerStreams {
		result[index] = plan.Mapping{Input: 0, Stream: id.String(), Output: 0}
	}
	return result, nil
}

// onlyManyPort names a shape's sole Many port in one direction. What a mapping
// needs is somewhere unambiguous to read the repeated stream descriptors from;
// whether the reader also takes a byte carrier is its own business, so a format
// that reads through a carrier maps the same way a direct one does.
func onlyManyPort(shape flow.Shape, direction flow.Direction) (string, bool) {
	ports := shape.Outputs
	if direction == flow.InputDirection {
		ports = shape.Inputs
	}
	result, found := "", false
	for _, port := range ports {
		if port.Multiplicity() != flow.ManyMultiplicity {
			continue
		}
		if found {
			return "", false
		}
		result, found = port.ID(), true
	}
	return result, found
}

func mappingStreams(values []stream.Descriptor) ([]stream.ID, error) {
	result := make([]stream.ID, len(values))
	seen := make(map[stream.ID]struct{}, len(values))
	for index, value := range values {
		if !value.Valid() || value.ID().IsZero() {
			return nil, stream.ErrInvalidDescriptor
		}
		if _, exists := seen[value.ID()]; exists {
			return nil, stream.ErrInvalidID
		}
		seen[value.ID()] = struct{}{}
		result[index] = value.ID()
	}
	return result, nil
}

func sameStreamSet(left, right []stream.ID) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[stream.ID]struct{}, len(left))
	for _, id := range left {
		if id.IsZero() {
			return false
		}
		seen[id] = struct{}{}
	}
	if len(seen) != len(left) {
		return false
	}
	for _, id := range right {
		if _, exists := seen[id]; !exists {
			return false
		}
		delete(seen, id)
	}
	return len(seen) == 0
}

func sameStreamOrder(left, right []stream.ID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func mappingError(code, message string, component plugin.Identity, detail map[string]string) error {
	return diagnostic.NewError(diagnostic.NewItem(code, diagnostic.ErrorSeverity, diagnostic.Path{Component: component.String()}, message, detail))
}
