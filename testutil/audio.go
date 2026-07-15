package testutil

import (
	"github.com/godexture/sdk/testutil/audio"
	"github.com/godexture/sdk/testutil/audio/pcm"
)

type CompareOptions = pcm.CompareOptions

type DecodeConfig = audio.DecodeConfig
type RoundtripConfig = audio.RoundtripConfig
type SnapshotConfig = audio.SnapshotConfig
type Buffer = audio.Buffer

var NewBuffer = audio.NewBuffer

var (
	SaveSnapshot = audio.SaveSnapshot
	LoadSnapshot = audio.LoadSnapshot
)

var (
	RunDecode         = audio.RunDecode
	RunSnapshotTests  = audio.RunSnapshotTests
	RunRoundtripTests = audio.RunRoundtripTests
)
