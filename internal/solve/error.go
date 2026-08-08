package solve

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
)

type limitError struct{ dimension string }

func (e limitError) Error() string { return "planning budget exhausted: " + e.dimension }

type canceledError struct{ cause error }

func (e canceledError) Error() string { return "planning canceled: " + e.cause.Error() }

type rejectError struct{ code string }

func (e rejectError) Error() string { return e.code }

type rejections map[string]int

func (r rejections) add(code string) {
	if code == "" {
		code = "unknown"
	}
	r[code]++
}

func (r rejections) summary() string {
	keys := make([]string, 0, len(r))
	for key := range r {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 8 {
		keys = keys[:8]
	}
	parts := make([]string, len(keys))
	for index, key := range keys {
		parts[index] = key + "=" + strconv.Itoa(r[key])
	}
	return strings.Join(parts, ",")
}

func (p *planner) planningError(err error, gap *graph.Gap, rejected rejections) error {
	switch value := err.(type) {
	case limitError:
		return solveDiagnostic("solve.budget-exhausted", gap, p.usage, p.budget, value.dimension, rejected)
	case canceledError:
		return solveDiagnostic("solve.canceled", gap, p.usage, p.budget, value.cause.Error(), rejected)
	case rejectError:
		return solveDiagnostic("solve.unsupported", gap, p.usage, p.budget, value.code, rejected)
	default:
		return err
	}
}

func solveDiagnostic(code string, gap *graph.Gap, usage plan.Usage, budget job.Budget, dimension string, rejected rejections) error {
	path := diagnostic.Path{}
	detail := map[string]string{
		"dimension":       dimension,
		"states":          strconv.Itoa(usage.States),
		"stateLimit":      strconv.Itoa(budget.States),
		"compiles":        strconv.Itoa(usage.Compiles),
		"compileLimit":    strconv.Itoa(budget.Compiles),
		"suggestions":     strconv.Itoa(usage.Suggestions),
		"suggestionLimit": strconv.Itoa(budget.SuggestionsPerNeed),
		"fixpoints":       strconv.Itoa(usage.FixpointIterations),
		"fixpointLimit":   strconv.Itoa(budget.FixpointIterations),
	}
	if gap != nil {
		path = diagnostic.Path{Component: gap.Node().String(), Descriptor: gap.Port()}
		detail["need"] = gap.Need().Code()
		if edge, ok := gap.Edge(); ok {
			detail["edge"] = edge.From().String() + "->" + edge.To().String()
		}
	}
	if summary := rejected.summary(); summary != "" {
		detail["rejections"] = summary
	}
	message := "requested graph cannot be planned"
	if code == "solve.budget-exhausted" {
		message = "planning budget was exhausted"
	} else if code == "solve.canceled" {
		message = "planning was canceled"
	} else if code == "solve.binding-unavailable" {
		message = "input/output binding is not available in this planner phase"
	} else if code == "solve.invalid-request" {
		message = "planning request is invalid"
	}
	return diagnostic.NewError(diagnostic.NewItem(code, diagnostic.ErrorSeverity, path, message, detail))
}

func rejectionCode(err error) string {
	if err == nil {
		return ""
	}
	if value, ok := err.(rejectError); ok {
		return value.code
	}
	items := diagnostic.ItemsOf(err)
	if len(items) != 0 {
		return items[0].Code
	}
	return fmt.Sprintf("%T", err)
}
