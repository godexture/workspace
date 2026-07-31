package testutil

import (
	"github.com/godexture/godec/sdk/testutil/audio"
	"github.com/godexture/godec/sdk/testutil/audio/pcm"
)

type CompareOptions = pcm.CompareOptions

type DecodeConfig = audio.DecodeConfig
type RoundtripConfig = audio.RoundtripConfig
type SnapshotConfig = audio.SnapshotConfig
type OutputTester = audio.OutputTester
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
