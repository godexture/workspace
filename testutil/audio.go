package testutil

import "github.com/godexture/sdk/testutil/audio"

type CompareOptions = audio.CompareOptions

type RoundtripConfig = audio.RoundtripConfig
type SnapshotConfig = audio.SnapshotConfig
type Buffer = audio.Buffer

var NewBuffer = audio.NewBuffer
var ComparePCM = audio.ComparePCM

var ResolveStreamInfo = audio.ResolveStreamInfo

var (
	EngineDemux  = audio.EngineDemux
	EngineDecode = audio.EngineDecode
	EngineEncode = audio.EngineEncode
	EngineMux    = audio.EngineMux
)

var (
	SaveSnapshot = audio.SaveSnapshot
	LoadSnapshot = audio.LoadSnapshot
)

var (
	EncodeWithFFmpeg = audio.EncodeWithFFmpeg
	DecodeWithFFmpeg = audio.DecodeWithFFmpeg
)

var (
	RunSnapshotTests  = audio.RunSnapshotTests
	RunRoundtripTests = audio.RunRoundtripTests
)
