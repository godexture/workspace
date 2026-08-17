// Format probing runs before any scenario: a Format subject has to reach a
// terminal probe status inside the declared budget.
package testkit

import (
	"context"
	"errors"
	"fmt"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/buffer"
	mediaformat "github.com/godexture/godec/media/format"
)

func verifyFormatProbe[I, O any](subject Subject[I, O], input Fixture[I], expectFailure bool) error {
	component, ok := componentOf(subject.set, subject.identity)
	if !ok {
		return fmt.Errorf("subject %s is absent from its Set", subject.identity)
	}
	trait, ok := mediaformat.ReadOf(component)
	if !ok || !trait.HasProbe() {
		return nil
	}
	data, err := carrierBytes(input.values)
	if err != nil {
		return err
	}
	budget := job.DefaultBudget()
	views := make([]access.ProbeView, 0)
	seen := make(map[[2]int64]struct{})
	var bytes int64
	for round := 1; round <= budget.ProbeRounds; round++ {
		result, probeErr := trait.Probe(mediaformat.NewProbeContextAtEnd(context.Background(), views, int64(len(data))))
		if probeErr != nil {
			return probeErr
		}
		if result.Status() != mediaformat.ProbeNeedsData {
			if !expectFailure && result.Status() != mediaformat.ProbeMatch && result.Status() != mediaformat.ProbeFallback {
				return fmt.Errorf("successful fixture produced terminal probe status %v", result.Status())
			}
			return nil
		}
		added := false
		for _, request := range result.Needs() {
			key := [2]int64{request.Offset(), request.Length()}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			bytes += request.Length()
			if bytes > int64(budget.ProbeBytes) {
				return fmt.Errorf("probe requested %d bytes, budget is %d", bytes, budget.ProbeBytes)
			}
			start := request.Offset()
			end := request.End()
			if start > int64(len(data)) {
				start = int64(len(data))
			}
			if end > int64(len(data)) {
				end = int64(len(data))
			}
			if request.Offset() < int64(len(data)) {
				view, viewErr := access.NewProbeViewAt(request.Offset(), data[start:end])
				if viewErr != nil {
					return viewErr
				}
				views = append(views, view)
			}
			added = true
		}
		if !added {
			return errors.New("probe repeated cached ranges without reaching a terminal status")
		}
	}
	return fmt.Errorf("probe exceeded %d rounds", budget.ProbeRounds)
}

func carrierBytes[T any](values []T) ([]byte, error) {
	var result []byte
	for _, value := range values {
		handle, ok := any(value).(buffer.Handle)
		if !ok || !handle.Valid() {
			return nil, errors.New("read Format fixture contains a non-byte payload")
		}
		result = handle.Bytes().AppendTo(result)
	}
	return result, nil
}
