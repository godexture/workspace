package config

import (
	"github.com/godexture/godec/sdk/testutil"
)

var (
	TestdataDir = "testdata"
)

var RoundtripCompareOptions = testutil.CompareOptions{
	MaxAbsDiff: 1.0 / 32768.0,
	MaxRMSE:    2e-5,
	MinSNR:     80.0,
}
