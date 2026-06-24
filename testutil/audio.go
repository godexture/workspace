package testutil

import "github.com/godexture/sdk/testutil/audio"

type CompareOptions = audio.CompareOptions

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
	RunSnapshotEncode        = audio.RunSnapshotEncode
	RunSnapshotDecode        = audio.RunSnapshotDecode
	RunRoundtripEncodeDecode = audio.RunRoundtripEncodeDecode
	RunRoundtripDecodeEncode = audio.RunRoundtripDecodeEncode
)
