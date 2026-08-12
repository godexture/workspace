package host

import (
	"sort"
	"strings"

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
			"selector": formatSelectorLabel(selector), "available": availableReadFormats(h.index),
		})
	}
	return catalog.FormatMatch{}, probeDiagnostic("prepare.format-ambiguous", boundary, plugin.Identity{}, "input Format hint matches multiple readable Format components", map[string]string{
		"selector": formatSelectorLabel(selector), "candidates": formatMatchList(matches),
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

func selectorMatchesFormat(selector job.FormatSelector, value mediaformat.Format) bool {
	if identity, ok := selector.Identity(); ok {
		return identity == value.Identity()
	}
	extension, ok := selector.Extension()
	if !ok {
		return false
	}
	for _, declared := range value.Extensions() {
		if declared == extension {
			return true
		}
	}
	return false
}

func formatSelectorLabel(selector job.FormatSelector) string {
	if identity, ok := selector.Identity(); ok {
		return "identity:" + identity.String()
	}
	if extension, ok := selector.Extension(); ok {
		return "extension:." + extension.String()
	}
	return ""
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
	seen := make(map[string]struct{})
	for _, component := range index.Components() {
		trait, ok := mediaformat.ReadOf(component)
		if ok && trait.Valid() {
			seen[trait.Format().Identity().String()] = struct{}{}
		}
	}
	values := make([]string, 0, len(seen))
	for value := range seen {
		values = append(values, value)
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}
