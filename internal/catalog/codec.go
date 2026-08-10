package catalog

import (
	"sort"

	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plugin"
)

type CodecBindingRole uint8

const (
	CodecRole CodecBindingRole = iota + 1
	ParserRole
)

type CodecBinding struct {
	tag  format.Tag
	role CodecBindingRole
}

func (b CodecBinding) Tag() format.Tag        { return b.tag }
func (b CodecBinding) Role() CodecBindingRole { return b.role }

func (i Index) CodecBindings(identity plugin.Identity) []CodecBinding {
	return append([]CodecBinding(nil), i.codecBindings[identity]...)
}

func indexCodecBindings(declarations []plugin.Declaration) map[plugin.Identity][]CodecBinding {
	result := make(map[plugin.Identity][]CodecBinding)
	for _, declaration := range declarations {
		tag, ok := codec.BindingTag(declaration.Key())
		if !ok {
			continue
		}
		for index, target := range declaration.Targets() {
			identity, ok := target.Component()
			if !ok {
				continue
			}
			role := CodecRole
			if index == 1 {
				role = ParserRole
			}
			result[identity] = append(result[identity], CodecBinding{tag: tag, role: role})
		}
	}
	for identity := range result {
		values := result[identity]
		sort.Slice(values, func(left, right int) bool {
			if values[left].tag != values[right].tag {
				return values[left].tag.String() < values[right].tag.String()
			}
			return values[left].role < values[right].role
		})
		result[identity] = values
	}
	return result
}
