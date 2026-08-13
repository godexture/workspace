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
		item := diagnostic.NewItem("prepare.format-not-found", diagnostic.ErrorSeverity, diagnostic.Path{}, "input Format hint does not match a readable Format in the Host catalog", formatDetail(boundary, "read", selector, availableReadFormats(h.index)))
		return catalog.FormatMatch{}, diagnostic.NewError(item.WithSuggestions(selectorSuggestions(selector, knownExtensions(h.index, readFormatOf))))
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
		item := diagnostic.NewItem("prepare.format-not-found", diagnostic.ErrorSeverity, diagnostic.Path{}, "output Format request does not match a writable Format in the Host catalog", formatDetail(boundary, "write", selector, availableWriteFormats(h.index)))
		return catalog.FormatMatch{}, diagnostic.NewError(item.WithSuggestions(selectorSuggestions(selector, knownExtensions(h.index, writeFormatOf))))
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

func readFormatOf(component plugin.Component) (mediaformat.Format, bool) {
	trait, ok := mediaformat.ReadOf(component)
	return trait.Format(), ok && trait.Valid()
}

func writeFormatOf(component plugin.Component) (mediaformat.Format, bool) {
	trait, ok := mediaformat.WriteOf(component)
	return trait.Format(), ok && trait.Valid()
}

func availableReadFormats(index catalog.Index) string {
	return availableFormats(index, readFormatOf)
}

func availableWriteFormats(index catalog.Index) string {
	return availableFormats(index, writeFormatOf)
}

// availableFormats answers the question the caller actually asked. A selector
// names an extension, so listing marker identities tells them nothing they can
// act on; the display name and the extensions the Format accepts do.
func availableFormats(index catalog.Index, declared func(plugin.Component) (mediaformat.Format, bool)) string {
	seen := make(map[plugin.Identity]string)
	for _, component := range index.Components() {
		value, ok := declared(component)
		if !ok {
			continue
		}
		if _, exists := seen[value.Identity()]; exists {
			continue
		}
		seen[value.Identity()] = describeFormat(component, value)
	}
	values := make([]string, 0, len(seen))
	for _, value := range seen {
		values = append(values, value)
	}
	sort.Strings(values)
	return strings.Join(values, ", ")
}

func describeFormat(component plugin.Component, value mediaformat.Format) string {
	name := component.Descriptor().DisplayName
	if name == "" {
		name = value.Identity().Name()
	}
	extensions := formatExtensions(value)
	if len(extensions) == 0 {
		return name
	}
	return name + " (" + strings.Join(extensions, ", ") + ")"
}

func formatExtensions(value mediaformat.Format) []string {
	values := value.Extensions()
	result := make([]string, len(values))
	for index, extension := range values {
		result[index] = "." + extension.String()
	}
	return result
}

// knownExtensions lists what a selector could have named, so a near miss can
// be suggested instead of only reported as absent.
func knownExtensions(index catalog.Index, declared func(plugin.Component) (mediaformat.Format, bool)) []string {
	seen := make(map[string]struct{})
	for _, component := range index.Components() {
		value, ok := declared(component)
		if !ok {
			continue
		}
		for _, extension := range value.Extensions() {
			seen["."+extension.String()] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
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

func formatDetail(boundary plan.Boundary, direction string, selector job.FormatSelector, available string) map[string]string {
	return map[string]string{
		"boundary":  boundary.Node,
		"scheme":    boundary.Scheme,
		"direction": direction,
		"selector":  selector.String(),
		"available": available,
	}
}

// selectorSuggestions offers near misses only for an extension, because that
// is the part a caller types.
func selectorSuggestions(selector job.FormatSelector, known []string) []string {
	extension, ok := selector.Extension()
	if !ok {
		return nil
	}
	return diagnostic.Suggest("."+extension.String(), known)
}
