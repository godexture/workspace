package catalog

import (
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plugin"
)

type FormatMatch struct {
	component plugin.Component
	format    mediaformat.Format
}

func (m FormatMatch) Component() plugin.Component { return m.component }
func (m FormatMatch) Format() mediaformat.Format  { return m.format }
func (m FormatMatch) Valid() bool                 { return !m.component.Identity().IsZero() && m.format.Valid() }

type formatIndex struct {
	readByID         map[plugin.Identity][]FormatMatch
	writeByID        map[plugin.Identity][]FormatMatch
	readByExtension  map[mediaformat.Extension][]FormatMatch
	writeByExtension map[mediaformat.Extension][]FormatMatch
}

func indexFormats(components []plugin.Component) formatIndex {
	result := formatIndex{
		readByID:         make(map[plugin.Identity][]FormatMatch),
		writeByID:        make(map[plugin.Identity][]FormatMatch),
		readByExtension:  make(map[mediaformat.Extension][]FormatMatch),
		writeByExtension: make(map[mediaformat.Extension][]FormatMatch),
	}
	for _, component := range components {
		if trait, ok := mediaformat.ReadOf(component); ok && trait.Valid() {
			match := FormatMatch{component: component, format: trait.Format()}
			result.readByID[match.format.Identity()] = append(result.readByID[match.format.Identity()], match)
			for _, extension := range match.format.Extensions() {
				result.readByExtension[extension] = append(result.readByExtension[extension], match)
			}
		}
		if trait, ok := mediaformat.WriteOf(component); ok && trait.Valid() {
			match := FormatMatch{component: component, format: trait.Format()}
			result.writeByID[match.format.Identity()] = append(result.writeByID[match.format.Identity()], match)
			for _, extension := range match.format.Extensions() {
				result.writeByExtension[extension] = append(result.writeByExtension[extension], match)
			}
		}
	}
	return result
}

func (i Index) ReadFormats(identity plugin.Identity) []FormatMatch {
	return cloneFormatMatches(i.formats.readByID[identity])
}

func (i Index) WriteFormats(identity plugin.Identity) []FormatMatch {
	return cloneFormatMatches(i.formats.writeByID[identity])
}

func (i Index) ReadExtension(extension mediaformat.Extension) []FormatMatch {
	return cloneFormatMatches(i.formats.readByExtension[extension])
}

func (i Index) WriteExtension(extension mediaformat.Extension) []FormatMatch {
	return cloneFormatMatches(i.formats.writeByExtension[extension])
}

func cloneFormatMatches(values []FormatMatch) []FormatMatch {
	return append([]FormatMatch(nil), values...)
}
