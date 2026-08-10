package format

import (
	"context"
	"errors"
	"strings"

	"github.com/godexture/godec/access"
)

var (
	ErrInvalidProbe     = errors.New("format probe is invalid")
	ErrProbeUnavailable = errors.New("format probe is unavailable")
)

// ProbeStatus distinguishes requests for more cached bytes from terminal
// format-selection outcomes.
type ProbeStatus uint8

const (
	ProbeNeedsData ProbeStatus = iota + 1
	ProbeMismatch
	ProbeFallback
	ProbeMatch
	ProbeMalformed
)

func (s ProbeStatus) Valid() bool { return s >= ProbeNeedsData && s <= ProbeMalformed }
func (s ProbeStatus) Terminal() bool {
	return s.Valid() && s != ProbeNeedsData
}

// Evidence is one deterministic content observation supporting a match.
// Locator, extension, and MIME hints are intentionally deferred to M6-5.
type Evidence struct{ detail string }

func NewEvidence(detail string) (Evidence, error) {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Evidence{}, ErrInvalidProbe
	}
	return Evidence{detail: detail}, nil
}

func (e Evidence) Valid() bool    { return strings.TrimSpace(e.detail) != "" }
func (e Evidence) Detail() string { return e.detail }

// ProbeResult is one immutable scheduler response. A result either requests
// ranges or terminates; it cannot do both.
type ProbeResult struct {
	status   ProbeStatus
	needs    []access.RangeRequest
	evidence []Evidence
	message  string
}

func Need(ranges ...access.RangeRequest) ProbeResult {
	return ProbeResult{status: ProbeNeedsData, needs: append([]access.RangeRequest(nil), ranges...)}
}

func Mismatch() ProbeResult { return ProbeResult{status: ProbeMismatch} }
func Fallback() ProbeResult { return ProbeResult{status: ProbeFallback} }

func Match(evidence ...Evidence) ProbeResult {
	return ProbeResult{status: ProbeMatch, evidence: append([]Evidence(nil), evidence...)}
}

func Malformed(message string, evidence ...Evidence) ProbeResult {
	return ProbeResult{status: ProbeMalformed, evidence: append([]Evidence(nil), evidence...), message: strings.TrimSpace(message)}
}

func (r ProbeResult) Valid() bool {
	if !r.status.Valid() {
		return false
	}
	for _, value := range r.needs {
		if !value.Valid() {
			return false
		}
	}
	for _, value := range r.evidence {
		if !value.Valid() {
			return false
		}
	}
	switch r.status {
	case ProbeNeedsData:
		return len(r.needs) != 0 && len(r.evidence) == 0 && r.message == ""
	case ProbeMismatch, ProbeFallback:
		return len(r.needs) == 0 && len(r.evidence) == 0 && r.message == ""
	case ProbeMatch:
		return len(r.needs) == 0 && len(r.evidence) != 0 && r.message == ""
	case ProbeMalformed:
		return len(r.needs) == 0 && len(r.evidence) != 0 && r.message != ""
	default:
		return false
	}
}

func (r ProbeResult) Status() ProbeStatus { return r.status }
func (r ProbeResult) Needs() []access.RangeRequest {
	return append([]access.RangeRequest(nil), r.needs...)
}
func (r ProbeResult) Evidence() []Evidence { return append([]Evidence(nil), r.evidence...) }
func (r ProbeResult) Message() string      { return r.message }

// ProbeContext exposes only immutable cached byte views and planning
// cancellation. End reports a source EOF learned while satisfying a range.
type ProbeContext struct {
	context  context.Context
	views    []access.ProbeView
	end      int64
	endKnown bool
}

func NewProbeContext(ctx context.Context, views []access.ProbeView) ProbeContext {
	return newProbeContext(ctx, views, 0, false)
}

func NewProbeContextAtEnd(ctx context.Context, views []access.ProbeView, end int64) ProbeContext {
	return newProbeContext(ctx, views, end, true)
}

func newProbeContext(ctx context.Context, views []access.ProbeView, end int64, endKnown bool) ProbeContext {
	if ctx == nil {
		ctx = context.Background()
	}
	return ProbeContext{
		context:  probeCancellationContext{Context: ctx},
		views:    append([]access.ProbeView(nil), views...),
		end:      end,
		endKnown: endKnown,
	}
}

func (c ProbeContext) Valid() bool {
	if c.context == nil || c.endKnown && c.end < 0 {
		return false
	}
	for _, view := range c.views {
		if !view.Valid() {
			return false
		}
		if c.endKnown && view.Base()+view.Size() > c.end {
			return false
		}
	}
	return true
}

func (c ProbeContext) Context() context.Context {
	if c.context == nil {
		return context.Background()
	}
	return c.context
}

func (c ProbeContext) Views() []access.ProbeView {
	return append([]access.ProbeView(nil), c.views...)
}

func (c ProbeContext) End() (int64, bool) { return c.end, c.endKnown }

type probeCancellationContext struct{ context.Context }

func (probeCancellationContext) Value(any) any { return nil }
