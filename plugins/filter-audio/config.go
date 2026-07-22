package filter

//go:generate config-generator --target FormatConfig --target ResampleConfig --target RemixConfig --target GainConfig --target NormalizeConfig --target FadeConfig --target DCOffsetConfig --target GateConfig --target TrimConfig --target SpeedConfig --target CompressorConfig --target EQConfig

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

type EQType = config.EQType

const (
	EQTypePeaking   = config.EQTypePeaking
	EQTypeLowShelf  = config.EQTypeLowShelf
	EQTypeHighShelf = config.EQTypeHighShelf
	EQTypeLowPass   = config.EQTypeLowPass
	EQTypeHighPass  = config.EQTypeHighPass
)

type GateMode = config.GateMode

const (
	GateModeHard    = config.GateModeHard
	GateModeLowpass = config.GateModeLowpass
)
