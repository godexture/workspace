package wav

import (
	"github.com/godexture/format-wav/internal"
)

type DemuxerConfig struct{}

func (DemuxerConfig) NodeConfiguration() {}

type MuxerConfig internal.MuxerConfig

func (MuxerConfig) NodeConfiguration() {}
