package mp4

import "github.com/godexture/godec/config"

type configID struct{}
type configuration struct{}

func configurationSchema() config.Schema[configuration] {
	return config.Struct[configID](func() configuration { return configuration{} }).Version("1").Build()
}
