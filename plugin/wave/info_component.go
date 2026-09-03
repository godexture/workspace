package wave

import (
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/plugin"
)

func infoComponent() plugin.Component {
	return plugin.NewComponent[infoID](
		plugin.Descriptor{DisplayName: "RIFF INFO metadata encoding"},
		configurationSchema(),
		plugin.WithTrait(infoRewriteTrait, "wave.riff-info-rewrite-layout=1", plugin.PortShapeOptional, struct{}{}),
		metadata.WithEncoding(parseInfo, marshalInfo, infoSupportedKeys()...),
	)
}
