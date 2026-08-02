// Package diagnostic contains structured, aggregatable diagnostics shared by
// the foundation packages.
//
// Detail values are metadata supplied by the caller. Callers must redact
// secrets before putting them in an Item; this package does not know which
// values are sensitive.
package diagnostic

import (
	"errors"
	"fmt"
	"strings"
)

// Severity describes the effect of a diagnostic.
type Severity uint8

const (
	InfoSeverity Severity = iota
	WarningSeverity
	ErrorSeverity
)

func (s Severity) String() string {
	switch s {
	case InfoSeverity:
		return "info"
	case WarningSeverity:
		return "warning"
	case ErrorSeverity:
		return "error"
	default:
		return "unknown"
	}
}

// Path identifies a component and the control-plane location associated with
// a diagnostic. Fields are ordered from the outer field to the nested field.
type Path struct {
	Component  string
	Descriptor string
	Fields     []string
}

// ComponentPath returns a path rooted at a component identity.
func ComponentPath(component string) Path {
	return Path{Component: component}
}

// DescriptorPath returns a path for a component descriptor property.
func DescriptorPath(component, property string) Path {
	return Path{Component: component, Descriptor: property}
}

// FieldPath returns a path for a field or nested field.
func FieldPath(fields ...string) Path {
	return Path{Fields: append([]string(nil), fields...)}
}

// WithComponent returns a copy rooted at component. An existing component is
// preserved when component is empty.
func (p Path) WithComponent(component string) Path {
	if component == "" || p.Component != "" {
		return p.clone()
	}
	p.Component = component
	return p.clone()
}

// Prefix prepends parent to p. The component and descriptor from parent take
// precedence when present.
func (p Path) Prefix(parent Path) Path {
	result := Path{
		Component:  p.Component,
		Descriptor: p.Descriptor,
		Fields:     append([]string(nil), p.Fields...),
	}
	if parent.Component != "" {
		result.Component = parent.Component
	}
	if parent.Descriptor != "" {
		result.Descriptor = parent.Descriptor
	}
	if len(parent.Fields) != 0 {
		result.Fields = append(append([]string(nil), parent.Fields...), result.Fields...)
	}
	return result
}

// IsZero reports whether p has no location information.
func (p Path) IsZero() bool {
	return p.Component == "" && p.Descriptor == "" && len(p.Fields) == 0
}

func (p Path) String() string {
	parts := make([]string, 0, 2+len(p.Fields))
	if p.Component != "" {
		parts = append(parts, p.Component)
	}
	if p.Descriptor != "" {
		parts = append(parts, "descriptor", p.Descriptor)
	}
	parts = append(parts, p.Fields...)
	return strings.Join(parts, ".")
}

func (p Path) clone() Path {
	p.Fields = append([]string(nil), p.Fields...)
	return p
}

// Item is one structured diagnostic. Code is a stable identifier intended for
// machine consumers; Message is a human-facing, already-redacted summary.
type Item struct {
	Code     string
	Severity Severity
	Path     Path
	Message  string
	Detail   map[string]string
}

// NewItem constructs an item and copies its detail map.
func NewItem(code string, severity Severity, path Path, message string, detail map[string]string) Item {
	return Item{
		Code:     code,
		Severity: severity,
		Path:     path.clone(),
		Message:  message,
		Detail:   cloneDetail(detail),
	}
}

// WithPath returns a copy of i rooted at path.
func (i Item) WithPath(path Path) Item {
	i.Path = path.clone()
	i.Detail = cloneDetail(i.Detail)
	return i
}

// Error aggregates one or more diagnostics. The zero value is ready for use.
type Error struct {
	items []Item
}

// NewError returns an aggregate containing items. Items are copied.
func NewError(items ...Item) *Error {
	result := &Error{}
	for _, item := range items {
		result.Add(item)
	}
	return result
}

// Add appends one item and returns e for convenient construction.
func (e *Error) Add(item Item) *Error {
	if e == nil {
		return NewError(item)
	}
	if item.Code == "" {
		item.Code = "diagnostic.missing-code"
	}
	item.Path = item.Path.clone()
	item.Detail = cloneDetail(item.Detail)
	e.items = append(e.items, item)
	return e
}

// Append adds diagnostics from err. Non-diagnostic errors are retained as a
// single generic item so aggregation never discards a failure.
func (e *Error) Append(err error) *Error {
	if err == nil {
		return e
	}
	var other *Error
	if errors.As(err, &other) && other != nil {
		for _, item := range other.Items() {
			e.Add(item)
		}
		return e
	}
	return e.Add(NewItem("diagnostic.wrapped-error", ErrorSeverity, Path{}, err.Error(), nil))
}

// Len reports the number of contained diagnostics.
func (e *Error) Len() int {
	if e == nil {
		return 0
	}
	return len(e.items)
}

// Empty reports whether e contains no diagnostics.
func (e *Error) Empty() bool {
	return e == nil || len(e.items) == 0
}

// Items returns a deep copy of the diagnostics.
func (e *Error) Items() []Item {
	if e == nil {
		return nil
	}
	items := make([]Item, len(e.items))
	for i, item := range e.items {
		items[i] = item
		items[i].Path = item.Path.clone()
		items[i].Detail = cloneDetail(item.Detail)
	}
	return items
}

func (e *Error) Error() string {
	if e == nil || len(e.items) == 0 {
		return ""
	}
	lines := make([]string, 0, len(e.items))
	for _, item := range e.items {
		location := item.Path.String()
		if location == "" {
			lines = append(lines, fmt.Sprintf("%s: %s: %s", item.Severity, item.Code, item.Message))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s: %s: %s", location, item.Severity, item.Code, item.Message))
	}
	return strings.Join(lines, "\n")
}

// ItemsOf returns structured diagnostics from err, or nil when err is not an
// aggregate diagnostic.
func ItemsOf(err error) []Item {
	var aggregate *Error
	if !errors.As(err, &aggregate) || aggregate == nil {
		return nil
	}
	return aggregate.Items()
}

func cloneDetail(detail map[string]string) map[string]string {
	if len(detail) == 0 {
		return nil
	}
	result := make(map[string]string, len(detail))
	for key, value := range detail {
		result[key] = value
	}
	return result
}
