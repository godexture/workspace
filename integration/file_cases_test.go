package integration_test

import (
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/file"
	"github.com/godexture/godec/testkit"
)

func runFileCases(t *testing.T, set plugin.Set, coverage *testkit.Coverage) {
	t.Helper()
	payload := make([]byte, 70*1024+37)
	for index := range payload {
		payload[index] = byte(index * 31)
	}
	testkit.Access(t,
		testkit.TrackAccess(testkit.AccessIn(set, file.SourceIdentity()), coverage),
		testkit.AccessCase{
			Name:  "random-read-with-stable-size",
			Input: testkit.LocalFile(payload),
			Want: testkit.WantAccess(payload,
				access.AllOf(access.RandomRead, access.StableSize),
				access.AllOf(access.SequentialRead),
			),
		},
	)
	testkit.Access(t,
		testkit.TrackAccess(testkit.AccessIn(set, file.SinkIdentity()), coverage),
		testkit.AccessCase{
			Name:  "sequential-atomic-replace",
			Input: testkit.LocalFile(payload),
			Want:  testkit.WantAccess(payload, access.AllOf(access.SequentialWrite)),
		},
		testkit.AccessCase{
			Name:  "random-atomic-replace",
			Input: testkit.LocalFile(payload[:257]),
			Want:  testkit.WantAccess(payload[:257], access.AllOf(access.RandomWrite)),
		},
	)
}
