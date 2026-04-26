package manifest

import "github.com/godexture/core/domain/media"

type Capability interface {
	MediaType() media.MediaType
	Match(p media.StreamInfo) bool
	Diagnose(p media.StreamInfo) bool
}
