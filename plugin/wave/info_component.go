package wave

import (
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/plugin"
)

func infoComponent() plugin.Component {
	return plugin.NewComponent[infoID](
		plugin.Descriptor{DisplayName: "RIFF INFO metadata encoding"},
		configurationSchema(),
		metadata.WithEncoding(parseInfo, marshalInfo),
	)
}
