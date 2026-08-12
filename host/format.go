package host

import (
	"sort"
	"strings"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/catalog"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

func (h *Host) resolveReadFormat(boundary plan.Boundary, selector job.FormatSelector) (catalog.FormatMatch, error) {
	matches := readFormatMatches(h.index, selector)
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return catalog.FormatMatch{}, probeDiagnostic("prepare.format-not-found", boundary, plugin.Identity{}, "input Format hint does not match a readable Format in the Host catalog", map[string]string{
			"selector": selector.String(), "available": availableReadFormats(h.index),
		})
	}
	return catalog.FormatMatch{}, probeDiagnostic("prepare.format-ambiguous", boundary, plugin.Identity{}, "input Format hint matches multiple readable Format components", map[string]string{
		"selector": selector.String(), "candidates": formatMatchList(matches),
	})
}

func (h *Host) resolveWriteFormat(boundary plan.Boundary, selector job.FormatSelector) (catalog.FormatMatch, error) {
	matches := writeFormatMatches(h.index, selector)
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return catalog.FormatMatch{}, formatSelectionDiagnostic("prepare.format-not-found", boundary, plugin.Identity{}, "output Format request does not match a writable Format in the Host catalog", map[string]string{
			"selector": selector.String(), "available": availableWriteFormats(h.index),
		})
	}
	return catalog.FormatMatch{}, formatSelectionDiagnostic("prepare.format-ambiguous", boundary, plugin.Identity{}, "output Format request matches multiple writable Format components", map[string]string{
		"selector": selector.String(), "candidates": formatMatchList(matches),
	})
}

func readFormatMatches(index catalog.Index, selector job.FormatSelector) []catalog.FormatMatch {
	if identity, ok := selector.Identity(); ok {
		return index.ReadFormats(identity)
	}
	if extension, ok := selector.Extension(); ok {
		return index.ReadExtension(extension)
	}
	return nil
}

func writeFormatMatches(index catalog.Index, selector job.FormatSelector) []catalog.FormatMatch {
	if identity, ok := selector.Identity(); ok {
		return index.WriteFormats(identity)
	}
	if extension, ok := selector.Extension(); ok {
		return index.WriteExtension(extension)
	}
	return nil
}

func formatMatchList(matches []catalog.FormatMatch) string {
	values := make([]string, len(matches))
	for index, match := range matches {
		values[index] = match.Format().Identity().String() + "@" + match.Component().Identity().String()
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}

func availableReadFormats(index catalog.Index) string {
	return availableFormats(index, func(component plugin.Component) (mediaformat.Format, bool) {
		trait, ok := mediaformat.ReadOf(component)
		return trait.Format(), ok && trait.Valid()
	})
}

func availableWriteFormats(index catalog.Index) string {
	return availableFormats(index, func(component plugin.Component) (mediaformat.Format, bool) {
		trait, ok := mediaformat.WriteOf(component)
		return trait.Format(), ok && trait.Valid()
	})
}

func availableFormats(index catalog.Index, declared func(plugin.Component) (mediaformat.Format, bool)) string {
	seen := make(map[string]struct{})
	for _, component := range index.Components() {
		value, ok := declared(component)
		if ok {
			seen[value.Identity().String()] = struct{}{}
		}
	}
	values := make([]string, 0, len(seen))
	for value := range seen {
		values = append(values, value)
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}

func formatSelectionDiagnostic(code string, boundary plan.Boundary, component plugin.Identity, message string, extra map[string]string) error {
	detail := map[string]string{
		"boundary":  boundary.Node,
		"scheme":    boundary.Scheme,
		"direction": "write",
	}
	for key, value := range extra {
		detail[key] = value
	}
	path := diagnostic.Path{}
	if !component.IsZero() {
		path.Component = component.String()
	}
	return diagnostic.NewError(diagnostic.NewItem(code, diagnostic.ErrorSeverity, path, message, detail))
}
