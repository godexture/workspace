package config

import (
	"fmt"
	"path/filepath"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/testutil"
)

type testProfile struct {
	Name  string
	Codec media.CodecID
	Attrs media.AudioAttributes
	testutil.CompareOptions
}

func newProfile(name string, codec media.CodecID, opts testutil.CompareOptions, channelLayout media.ChannelLayout, format media.SampleFormat, sampleRate int) testProfile {
	attrs := media.AudioAttributes{
		SampleRate:    sampleRate,
		ChannelLayout: channelLayout,
		Format:        format,
	}

	return testProfile{
		Name:           name,
		Codec:          codec,
		Attrs:          attrs,
		CompareOptions: opts,
	}
}

var Profiles = []testProfile{
	newProfile("lpcm.wav", media.CodecLPCM, testutil.CompareOptions{MaxAbsDiff: 1e-4, MaxRMSE: 1e-4, MinSNR: 75.0}, media.LayoutStereo2_0, media.SampleFormatF32, 48000),
	newProfile("pcmu.wav", media.CodecPCMU, testutil.CompareOptions{MaxAbsDiff: 0.032, MaxRMSE: 0.01, MinSNR: 35.0}, media.LayoutStereo2_0, media.SampleFormatS16, 48000),
	newProfile("pcma.wav", media.CodecPCMA, testutil.CompareOptions{MaxAbsDiff: 0.032, MaxRMSE: 0.01, MinSNR: 35.0}, media.LayoutStereo2_0, media.SampleFormatS16, 48000),
	newProfile("msadpcm.wav", media.CodecMSADPCM, testutil.CompareOptions{MaxAbsDiff: 0.15, MaxRMSE: 2e-3, MinSNR: 35.0}, media.LayoutStereo2_0, media.SampleFormatS16, 48000),
	newProfile("imaadpcm.wav", media.CodecIMAADPCM, testutil.CompareOptions{MaxAbsDiff: 0.15, MaxRMSE: 4e-3, MinSNR: 35.0}, media.LayoutStereo2_0, media.SampleFormatS16, 48000),
}

var (
	SourcePath  = "source.wav"
	TestdataDir = "testdata"
	SnapshotDir = filepath.Join(TestdataDir, "snapshots")
)

func BuildTestdataPath(fileName string) string {
	return filepath.Join(TestdataDir, fileName)
}

func BuildSnapshotPath(fileName string) string {
	return filepath.Join(SnapshotDir, fileName+".snapshot")
}

func EnumerateTestdataFiles() []string {
	paths, err := filepath.Glob(TestdataDir)
	if err != nil {
		panic(fmt.Errorf("failed to glob test files: %v", err))
	}

	fileNames := make([]string, len(paths))
	for i, path := range paths {
		fileNames[i] = filepath.Base(path)
	}

	return fileNames
}
