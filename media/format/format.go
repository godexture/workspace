// Package format defines open container/elementary-stream contracts.
package format

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/plugin"
)

type Tag string

func NewTag(namespace, value string) Tag {
	if namespace == "" {
		return Tag(value)
	}
	if value == "" {
		return Tag(namespace)
	}
	return Tag(namespace + ":" + value)
}

func (t Tag) Valid() bool    { return t != "" }
func (t Tag) String() string { return string(t) }

// Format is a declarative format contract. It does not own byte locations,
// decoder implementations, metadata parsers, or access providers.
type Format struct {
	identity   plugin.Identity
	carriers   []carrier.ID
	extensions []Extension
	packetized bool
}

type Option func(*formatOptions)

type formatOptions struct {
	extensions []string
}

// WithExtensions declares canonical file-name hints for this Format.
func WithExtensions(values ...string) Option {
	return func(options *formatOptions) { options.extensions = append(options.extensions, values...) }
}

func Define[Marker any](carriers []carrier.ID, values ...Option) (Format, error) {
	result := Format{identity: plugin.IdentityOf[Marker]()}
	if err := result.set(carriers, values); err != nil {
		return Format{}, err
	}
	return result, nil
}

func DefinePacketized[Marker any](carriers []carrier.ID, values ...Option) (Format, error) {
	result := Format{identity: plugin.IdentityOf[Marker](), packetized: true}
	if err := result.set(carriers, values); err != nil {
		return Format{}, err
	}
	return result, nil
}

func (f *Format) set(carriers []carrier.ID, values []Option) error {
	if f.identity.IsZero() {
		return errors.New("format marker identity must be valid")
	}
	options := formatOptions{}
	for _, option := range values {
		if option != nil {
			option(&options)
		}
	}
	extensions, err := canonicalExtensions(options.extensions)
	if err != nil {
		return err
	}
	seen := make(map[carrier.ID]struct{}, len(carriers))
	for _, id := range carriers {
		if !id.Valid() {
			return errors.New("format carrier identity is required")
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("format carrier %q is repeated", id)
		}
		seen[id] = struct{}{}
	}
	f.carriers = append([]carrier.ID(nil), carriers...)
	f.extensions = extensions
	return nil
}

func (f Format) Valid() bool               { return !f.identity.IsZero() }
func (f Format) Identity() plugin.Identity { return f.identity }
func (f Format) Carriers() []carrier.ID    { return append([]carrier.ID(nil), f.carriers...) }
func (f Format) Extensions() []Extension   { return append([]Extension(nil), f.extensions...) }
func (f Format) Packetized() bool          { return f.packetized }

// Same reports whether two values make the same declaration for one Format
// identity.
func (f Format) Same(other Format) bool {
	return f.identity == other.identity && f.packetized == other.packetized && slices.Equal(f.carriers, other.carriers) && slices.Equal(f.extensions, other.extensions)
}

func (f Format) manifest() string {
	carriers := make([]string, len(f.carriers))
	for index, value := range f.carriers {
		carriers[index] = value.String()
	}
	extensions := make([]string, len(f.extensions))
	for index, value := range f.extensions {
		extensions[index] = value.String()
	}
	return "format=" + f.identity.String() + "|packetized=" + fmt.Sprint(f.packetized) + "|carriers=" + strings.Join(carriers, ",") + "|extensions=" + strings.Join(extensions, ",")
}
