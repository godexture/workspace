package file_test

import (
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/file"
	"github.com/godexture/godec/testkit"
)

func TestPublicAccessConformance(t *testing.T) {
	definition := file.Plugin()
	coverage := testkit.NewCoverage()
	payload := make([]byte, 70*1024+37)
	for index := range payload {
		payload[index] = byte(index * 31)
	}

	testkit.Access(t,
		testkit.TrackAccess(testkit.AccessOf(definition, file.SourceIdentity()), coverage),
		testkit.AccessCase{
			Name:  "random-read-with-stable-size",
			Input: testkit.LocalFile(payload),
			Want: testkit.WantAccess(payload,
				access.AnyOf(access.RandomRead, access.StableSize),
				access.AnyOf(access.SequentialRead),
			),
		},
	)
	testkit.Access(t,
		testkit.TrackAccess(testkit.AccessOf(definition, file.SinkIdentity()), coverage),
		testkit.AccessCase{
			Name:  "sequential-atomic-replace",
			Input: testkit.LocalFile(payload),
			Want:  testkit.WantAccess(payload, access.AnyOf(access.SequentialWrite)),
		},
		testkit.AccessCase{
			Name:  "random-atomic-replace",
			Input: testkit.LocalFile(payload[:257]),
			Want:  testkit.WantAccess(payload[:257], access.AnyOf(access.RandomWrite)),
		},
	)
	coverage.VerifyExecutable(t, plugin.NewSet(definition))
}
