package job

import (
	"errors"

	"github.com/godexture/godec/config"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plugin"
)

type FormatSelectorKind uint8

const (
	FormatIdentitySelector FormatSelectorKind = iota + 1
	FormatExtensionSelector
)

func (k FormatSelectorKind) Valid() bool {
	return k == FormatIdentitySelector || k == FormatExtensionSelector
}

// FormatSelector identifies one Format by its stable identity or by a
// canonical file-name extension. Resolution belongs to the Host catalog.
type FormatSelector struct {
	kind      FormatSelectorKind
	identity  plugin.Identity
	extension mediaformat.Extension
	config    config.Patch
	configSet bool
}

func SelectFormat(value mediaformat.Format) (FormatSelector, error) {
	if !value.Valid() {
		return FormatSelector{}, errors.New("job Format selector is invalid")
	}
	return FormatSelector{kind: FormatIdentitySelector, identity: value.Identity()}, nil
}

func SelectFormatExtension(extension mediaformat.Extension) (FormatSelector, error) {
	if !extension.Valid() {
		return FormatSelector{}, errors.New("job Format extension selector is invalid")
	}
	return FormatSelector{kind: FormatExtensionSelector, extension: extension}, nil
}

// WithConfig returns a selector with an explicit sparse component config.
// Calling it with an empty patch is distinct from omitting config.
func (s FormatSelector) WithConfig(patch config.Patch) FormatSelector {
	result := s.clone()
	result.config = patch.Clone()
	result.configSet = true
	return result
}

func (s FormatSelector) Valid() bool {
	switch s.kind {
	case FormatIdentitySelector:
		return !s.identity.IsZero() && !s.extension.Valid()
	case FormatExtensionSelector:
		return s.identity.IsZero() && s.extension.Valid()
	default:
		return false
	}
}

func (s FormatSelector) Kind() FormatSelectorKind { return s.kind }

func (s FormatSelector) Identity() (plugin.Identity, bool) {
	return s.identity, s.kind == FormatIdentitySelector && !s.identity.IsZero()
}

func (s FormatSelector) Extension() (mediaformat.Extension, bool) {
	return s.extension, s.kind == FormatExtensionSelector && s.extension.Valid()
}

func (s FormatSelector) Config() (config.Patch, bool) {
	return s.config.Clone(), s.configSet
}

// Matches reports whether this selector names the supplied Format
// declaration. It does not resolve component implementation ambiguity.
func (s FormatSelector) Matches(value mediaformat.Format) bool {
	if identity, ok := s.Identity(); ok {
		return identity == value.Identity()
	}
	extension, ok := s.Extension()
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

func (s FormatSelector) String() string {
	if identity, ok := s.Identity(); ok {
		return "identity:" + identity.String()
	}
	if extension, ok := s.Extension(); ok {
		return "extension:." + extension.String()
	}
	return ""
}

func (s FormatSelector) clone() FormatSelector {
	result := s
	result.config = s.config.Clone()
	return result
}
