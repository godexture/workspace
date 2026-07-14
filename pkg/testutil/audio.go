package testutil

import "github.com/godexture/sdk/testutil/audio"

type CompareOptions = audio.CompareOptions

type RoundtripConfig = audio.RoundtripConfig
type SnapshotConfig = audio.SnapshotConfig
type Buffer = audio.Buffer

var NewBuffer = audio.NewBuffer
var ComparePCM = audio.ComparePCM

var (
	DownmixToMono   = audio.DownmixToMono
	Resample16kTo8k = audio.Resample16kTo8k
)

var (
	EncodeToMuxer   = audio.EncodeToMuxer
	DecodeToFloat32 = audio.DecodeToFloat32
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
