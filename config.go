package filter

//go:generate config-generator --target FormatConfig --target ResampleConfig --target RemixConfig --target GainConfig --target NormalizeConfig --target FadeConfig --target DCOffsetConfig --target TrimConfig --target SpeedConfig

import (
	"github.com/godexture/filter-audio/internal/config"
)

type SpeedMode = config.SpeedMode

const (
	SpeedModeInterpolate = config.SpeedModeInterpolate
	SpeedModeRelabel     = config.SpeedModeRelabel
)

type TrimMode = config.TrimMode

const (
	TrimModeBoth  = config.TrimModeBoth
	TrimModeStart = config.TrimModeStart
	TrimModeEnd   = config.TrimModeEnd
)
