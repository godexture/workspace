package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
	internalplanning "github.com/godexture/godec/internal/planning"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/buffer"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type formatChoice struct {
	component plugin.Component
	trait     mediaformat.ReadTrait
	fallback  bool
	evidence  []mediaformat.Evidence
}

type probeCandidate struct {
	component plugin.Component
	trait     mediaformat.ReadTrait
	result    mediaformat.ProbeResult
	terminal  bool
}

type probeStore struct {
	allocator  *buffer.Allocator
	handles    []buffer.Handle
	views      []access.ProbeView
	random     access.Random
	sequential access.Sequential
	offset     int64
	end        int64
	endKnown   bool
	read       resource.Bytes
}

func newProbeStore(opening access.Opening, budget job.Budget) (*probeStore, error) {
	if !opening.Valid() || opening.Direction() != access.SourceDirection || !budget.Valid() {
		return nil, errors.New("probe store requires a selected source opening and valid budget")
	}
	allocator, err := buffer.NewAllocator(int64(budget.ProbeBytes))
	if err != nil {
		return nil, err
	}
	random, _ := access.RandomOf(opening)
	sequential, _ := access.SequentialOf(opening)
	if random == nil && sequential == nil {
		return nil, errors.New("probe store received no readable source view")
	}
	return &probeStore{allocator: allocator, random: random, sequential: sequential}, nil
}

func (h *Host) probeInput(ctx context.Context, boundary plan.Boundary, session acquiredSession, budget job.Budget) (formatChoice, *probeStore, plan.Usage, error) {
	candidates := make([]probeCandidate, 0)
	for _, component := range h.index.Components() {
		trait, ok := mediaformat.ReadOf(component)
		if ok && trait.Valid() && trait.HasProbe() {
			candidates = append(candidates, probeCandidate{component: component, trait: trait})
		}
	}
	sort.Slice(candidates, func(left, right int) bool {
		return candidates[left].component.Identity().String() < candidates[right].component.Identity().String()
	})
	if len(candidates) == 0 {
		return formatChoice{}, nil, plan.Usage{}, probeDiagnostic("prepare.probe-candidate", boundary, plugin.Identity{}, "automatic Format selection has no Probe candidates", nil)
	}
	store, err := newProbeStore(session.opening, budget)
	if err != nil {
		return formatChoice{}, nil, plan.Usage{}, probeDiagnostic("prepare.probe-opening", boundary, plugin.Identity{}, "automatic Format Probe cannot use the acquired source opening", map[string]string{"cause": err.Error()})
	}
	usage := plan.Usage{}
	fail := func(err error) (formatChoice, *probeStore, plan.Usage, error) {
		closeErr := store.Close()
		return formatChoice{}, nil, usage, errors.Join(err, closeErr)
	}

	for {
		if internalplanning.DurationExhausted(ctx) {
			return fail(probeBudgetDiagnostic(boundary, "duration", usage, budget, candidates))
		}
		if usage.ProbeRounds >= budget.ProbeRounds {
			return fail(probeBudgetDiagnostic(boundary, "rounds", usage, budget, candidates))
		}
		usage.ProbeRounds++
		var needs []access.RangeRequest
		unresolved := 0
		for index := range candidates {
			candidate := &candidates[index]
			if candidate.terminal {
				continue
			}
			unresolved++
			probeContext := mediaformat.NewProbeContext(ctx, store.Views())
			if store.endKnown {
				probeContext = mediaformat.NewProbeContextAtEnd(ctx, store.Views(), store.end)
			}
			failure := invoke(ctx, PreparePhase, boundary.Node, "format/probe/"+candidate.component.Identity().String(), func(context.Context) error {
				var probeErr error
				candidate.result, probeErr = candidate.trait.Probe(probeContext)
				return probeErr
			})
			if failure != nil {
				if internalplanning.DurationExhausted(ctx) {
					return fail(probeBudgetDiagnostic(boundary, "duration", usage, budget, candidates))
				}
				return fail(*failure)
			}
			switch candidate.result.Status() {
			case mediaformat.ProbeNeedsData:
				needs = append(needs, candidate.result.Needs()...)
			case mediaformat.ProbeMalformed:
				return fail(probeDiagnostic("prepare.probe-malformed", boundary, candidate.component.Identity(), "Format Probe recognized malformed input", map[string]string{
					"format":   candidate.trait.Format().Identity().String(),
					"cause":    candidate.result.Message(),
					"evidence": evidenceList(candidate.result.Evidence()),
				}))
			default:
				candidate.terminal = true
			}
		}
		if unresolved == 0 {
			break
		}
		if len(needs) == 0 {
			break
		}
		progress, unmet, extendErr := store.Extend(ctx, needs, budget.ProbeBytes)
		usage.ProbeBytes = store.read
		if extendErr != nil {
			if internalplanning.DurationExhausted(ctx) {
				return fail(probeBudgetDiagnosticWithRange(boundary, "duration", usage, budget, candidates, unmet))
			}
			if errors.Is(extendErr, buffer.ErrLimit) {
				return fail(probeBudgetDiagnosticWithRange(boundary, "bytes", usage, budget, candidates, unmet))
			}
			return fail(probeDiagnostic("prepare.probe-read", boundary, plugin.Identity{}, "Format Probe could not read a requested source range", map[string]string{
				"range": rangeString(unmet), "cause": extendErr.Error(),
			}))
		}
		if !progress {
			return fail(probeDiagnostic("prepare.probe-progress", boundary, plugin.Identity{}, "Format Probe repeated a satisfied or unavailable range without making progress", map[string]string{
				"range": rangeString(unmet), "rounds": strconv.Itoa(usage.ProbeRounds),
			}))
		}
	}
	usage.ProbeBytes = store.read
	choice, selectErr := chooseFormat(boundary, candidates)
	if selectErr != nil {
		return fail(selectErr)
	}
	return choice, store, usage, nil
}

func chooseFormat(boundary plan.Boundary, candidates []probeCandidate) (formatChoice, error) {
	var matches, fallbacks []probeCandidate
	for _, candidate := range candidates {
		switch candidate.result.Status() {
		case mediaformat.ProbeMatch:
			matches = append(matches, candidate)
		case mediaformat.ProbeFallback:
			fallbacks = append(fallbacks, candidate)
		}
	}
	selected := matches
	fallback := false
	if len(selected) == 0 {
		selected = fallbacks
		fallback = true
	}
	if len(selected) == 0 {
		return formatChoice{}, probeDiagnostic("prepare.probe-mismatch", boundary, plugin.Identity{}, "no Format Probe recognized the input and no fallback is available", nil)
	}
	if len(selected) != 1 {
		type ambiguity struct {
			identity string
			evidence string
		}
		values := make([]ambiguity, len(selected))
		for index, candidate := range selected {
			values[index] = ambiguity{identity: candidate.component.Identity().String(), evidence: evidenceList(candidate.result.Evidence())}
		}
		sort.Slice(values, func(left, right int) bool { return values[left].identity < values[right].identity })
		identities := make([]string, len(values))
		evidence := make([]string, len(values))
		for index, value := range values {
			identities[index] = value.identity
			evidence[index] = value.identity + "=" + value.evidence
		}
		return formatChoice{}, probeDiagnostic("prepare.probe-ambiguous", boundary, plugin.Identity{}, "multiple Format Probe candidates have the same highest confidence", map[string]string{
			"candidates": strings.Join(identities, ","), "confidence": probeConfidence(fallback), "evidence": strings.Join(evidence, ","),
		})
	}
	candidate := selected[0]
	return formatChoice{
		component: candidate.component,
		trait:     candidate.trait,
		fallback:  fallback,
		evidence:  candidate.result.Evidence(),
	}, nil
}

func probeConfidence(fallback bool) string {
	if fallback {
		return "fallback"
	}
	return "content"
}

func (s *probeStore) Views() []access.ProbeView {
	if s == nil {
		return nil
	}
	return append([]access.ProbeView(nil), s.views...)
}

func (s *probeStore) Extend(ctx context.Context, requests []access.RangeRequest, limit resource.Bytes) (bool, access.RangeRequest, error) {
	if s == nil || s.allocator == nil {
		return false, access.RangeRequest{}, errors.New("probe store is closed")
	}
	requests = canonicalRanges(requests)
	progress := false
	var unmet access.RangeRequest
	if s.random != nil {
		for _, request := range requests {
			unmet = request
			for _, missing := range s.missing(request) {
				if resource.Bytes(s.allocator.Used())+resource.Bytes(missing.Length()) > limit {
					return progress, missing, buffer.ErrLimit
				}
				changed, err := s.readRange(ctx, missing, true)
				if err != nil {
					return progress, missing, err
				}
				progress = progress || changed
			}
		}
		return progress, unmet, nil
	}
	maximum := s.offset
	for _, request := range requests {
		if request.End() > maximum {
			maximum = request.End()
			unmet = request
		}
	}
	if s.endKnown && maximum > s.end {
		maximum = s.end
	}
	if maximum <= s.offset {
		return false, unmet, nil
	}
	request, err := access.NewRangeRequest(s.offset, maximum-s.offset)
	if err != nil {
		return false, unmet, err
	}
	if resource.Bytes(s.allocator.Used())+resource.Bytes(request.Length()) > limit {
		return false, request, buffer.ErrLimit
	}
	progress, err = s.readRange(ctx, request, false)
	return progress, request, err
}

func (s *probeStore) readRange(ctx context.Context, request access.RangeRequest, positioned bool) (bool, error) {
	if !request.Valid() || request.Length() > int64(math.MaxInt) {
		return false, access.ErrInvalidProbeRange
	}
	lease, err := s.allocator.Overwrite(buffer.Spec{Alignment: 1, ReadOnly: true, Planes: []buffer.PlaneSpec{{Size: int(request.Length())}}})
	if err != nil {
		return false, err
	}
	count := 0
	reachedEnd := false
	err = lease.Fill(func(destination buffer.Mutable) error {
		bytes := destination.Bytes()
		for count < len(bytes) {
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			var n int
			var readErr error
			if positioned {
				n, readErr = s.random.ReadAt(ctx, bytes[count:], request.Offset()+int64(count))
			} else {
				n, readErr = s.sequential.Read(ctx, bytes[count:])
			}
			if n < 0 || n > len(bytes)-count {
				return errors.New("probe source returned an invalid read count")
			}
			count += n
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					reachedEnd = true
					return nil
				}
				return readErr
			}
			if n == 0 {
				return io.ErrNoProgress
			}
		}
		return nil
	})
	if err != nil {
		lease.Discard()
		return false, err
	}
	handle, err := lease.Commit()
	if err != nil {
		return false, err
	}
	if count == 0 {
		handle.Release()
	} else {
		if count != int(request.Length()) {
			exact, rangeErr := handle.Range(0, count)
			handle.Release()
			if rangeErr != nil {
				return false, rangeErr
			}
			handle = exact
		}
		view, viewErr := access.NewProbeViewFromBuffer(request.Offset(), handle.Borrow())
		if viewErr != nil {
			handle.Release()
			return false, viewErr
		}
		s.handles = append(s.handles, handle)
		s.views = append(s.views, view)
		s.read += resource.Bytes(count)
		sort.SliceStable(s.views, func(left, right int) bool { return s.views[left].Base() < s.views[right].Base() })
	}
	end := request.Offset() + int64(count)
	if !positioned {
		s.offset = end
	}
	progress := count != 0
	if reachedEnd {
		if !s.endKnown || end < s.end {
			s.end = end
			s.endKnown = true
			progress = true
		}
	}
	return progress, nil
}

func (s *probeStore) missing(request access.RangeRequest) []access.RangeRequest {
	end := request.End()
	if s.endKnown && end > s.end {
		end = s.end
	}
	cursor := request.Offset()
	if cursor >= end {
		return nil
	}
	var missing []access.RangeRequest
	for _, view := range s.views {
		viewEnd := view.Base() + view.Size()
		if viewEnd <= cursor || view.Base() >= end {
			continue
		}
		if view.Base() > cursor {
			value, _ := access.NewRangeRequest(cursor, view.Base()-cursor)
			missing = append(missing, value)
		}
		if viewEnd > cursor {
			cursor = viewEnd
		}
		if cursor >= end {
			break
		}
	}
	if cursor < end {
		value, _ := access.NewRangeRequest(cursor, end-cursor)
		missing = append(missing, value)
	}
	return missing
}

func (s *probeStore) Close() error {
	if s == nil {
		return nil
	}
	for index := len(s.handles) - 1; index >= 0; index-- {
		s.handles[index].Release()
	}
	s.handles = nil
	s.views = nil
	if s.allocator != nil && s.allocator.Used() != 0 {
		return fmt.Errorf("probe allocator retained %d bytes", s.allocator.Used())
	}
	s.allocator = nil
	return nil
}

func canonicalRanges(values []access.RangeRequest) []access.RangeRequest {
	result := append([]access.RangeRequest(nil), values...)
	sort.Slice(result, func(left, right int) bool {
		if result[left].Offset() != result[right].Offset() {
			return result[left].Offset() < result[right].Offset()
		}
		return result[left].Length() < result[right].Length()
	})
	write := 0
	for _, value := range result {
		if write != 0 && value.Offset() == result[write-1].Offset() && value.Length() == result[write-1].Length() {
			continue
		}
		result[write] = value
		write++
	}
	return result[:write]
}

func probeDiagnostic(code string, boundary plan.Boundary, component plugin.Identity, message string, extra map[string]string) error {
	detail := map[string]string{"boundary": boundary.Node, "scheme": boundary.Scheme, "direction": "read"}
	for key, value := range extra {
		detail[key] = value
	}
	path := diagnostic.Path{Component: component.String()}
	return diagnostic.NewError(diagnostic.NewItem(code, diagnostic.ErrorSeverity, path, message, detail))
}

func probeBudgetDiagnostic(boundary plan.Boundary, dimension string, usage plan.Usage, budget job.Budget, candidates []probeCandidate) error {
	return probeBudgetDiagnosticWithRange(boundary, dimension, usage, budget, candidates, access.RangeRequest{})
}

func probeBudgetDiagnosticWithRange(boundary plan.Boundary, dimension string, usage plan.Usage, budget job.Budget, candidates []probeCandidate, unmet access.RangeRequest) error {
	pending := make([]string, 0)
	for _, candidate := range candidates {
		if !candidate.terminal {
			pending = append(pending, candidate.component.Identity().String())
		}
	}
	return probeDiagnostic("prepare.probe-budget", boundary, plugin.Identity{}, "Format Probe planning budget was exhausted", map[string]string{
		"dimension":     dimension,
		"bytes":         strconv.FormatUint(uint64(usage.ProbeBytes), 10),
		"byteLimit":     strconv.FormatUint(uint64(budget.ProbeBytes), 10),
		"rounds":        strconv.Itoa(usage.ProbeRounds),
		"roundLimit":    strconv.Itoa(budget.ProbeRounds),
		"durationLimit": budget.Duration.String(),
		"candidate":     strings.Join(pending, ","),
		"range":         rangeString(unmet),
	})
}

func rangeString(value access.RangeRequest) string {
	if !value.Valid() {
		return ""
	}
	return strconv.FormatInt(value.Offset(), 10) + ":" + strconv.FormatInt(value.End(), 10)
}

func evidenceList(values []mediaformat.Evidence) string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.Detail()
	}
	return strings.Join(result, ",")
}
