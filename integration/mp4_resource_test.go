package integration_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/resource"
	"github.com/godexture/godec/standard"
)

// TestMP4RemuxResourcesDoNotScaleWithSamples is the M7-C12 resource gate. The
// 1k and 1M fixtures differ only in sample count: same track, same tables, one
// chunk each. Every reservation the Plan makes must therefore be identical, and
// the journal must be charged for the one chunk rather than for the samples.
func TestMP4RemuxResourcesDoNotScaleWithSamples(t *testing.T) {
	small := mp4RemuxReservation(t, 1_000)
	large := mp4RemuxReservation(t, 1_000_000)
	if small.memory != large.memory {
		t.Fatalf("buffer grant = %d for 1k samples, %d for 1M", small.memory, large.memory)
	}
	if small.scratch != large.scratch {
		t.Fatalf("scratch reservation = %d for 1k samples, %d for 1M", small.scratch, large.scratch)
	}
	// One chunk is one 64-bit journal entry. A journal that grew with samples
	// would be the same defect as growing the in-memory tables.
	if small.scratch.Reserved != 8 {
		t.Fatalf("chunk-offset journal reserved %d bytes for one chunk", small.scratch.Reserved)
	}
	// The reservation is the node claims alone: a file job asks for no output
	// boundary spool, so nothing else may be folded into the same quota.
	if small.scratch.Reserved != small.claims {
		t.Fatalf("scratch reserved %d bytes for %d bytes of node claims", small.scratch.Reserved, small.claims)
	}
}

// TestMP4RemuxJournalGrowsOnDiskNotInMemory is the other half of the M7-C12
// gate. Sample count is the axis a single-chunk fixture can vary; chunk count
// is the one a real movie varies, because an encoder starts a new chunk every
// so often. The journal is the state that is allowed to grow with it, and it
// grows on disk under the quota while the memory grant stays put.
//
// 1,024 entries is exactly one journal page, so the small case also pins the
// boundary where the page is flushed on the entry that fills it.
func TestMP4RemuxJournalGrowsOnDiskNotInMemory(t *testing.T) {
	const page, many = 1_024, 200_000
	small := mp4RemuxReservationOf(t, mp4ManyChunkFixture(page))
	large := mp4RemuxReservationOf(t, mp4ManyChunkFixture(many))
	if small.memory != large.memory {
		t.Fatalf("buffer grant = %d for %d chunks, %d for %d", small.memory, page, large.memory, many)
	}
	for _, value := range []struct {
		reservation mp4Reservation
		chunks      int64
	}{{small, page}, {large, many}} {
		if want := resource.Bytes(value.chunks * 8); value.reservation.scratch.Reserved != want {
			t.Fatalf("%d chunks reserved %d journal bytes, want %d", value.chunks, value.reservation.scratch.Reserved, want)
		}
		if value.reservation.scratch.Reserved > value.reservation.scratch.Limit {
			t.Fatalf("%d chunks reserved %d journal bytes past the %d limit", value.chunks, value.reservation.scratch.Reserved, value.reservation.scratch.Limit)
		}
	}
}

// TestMP4RemuxFailsWhenTheJournalHasNoQuota keeps the journal claim honest. A
// preset with scratch disabled cannot satisfy the mux, and that has to be a
// planning failure rather than a remux that quietly skips patching offsets.
func TestMP4RemuxFailsWhenTheJournalHasNoQuota(t *testing.T) {
	policy, ok := job.PolicyFor(job.Realtime)
	if !ok {
		t.Fatal("realtime policy is unavailable")
	}
	if policy.Resources.ScratchMaxBytes != 0 {
		t.Fatalf("realtime scratch limit = %d, want disabled", policy.Resources.ScratchMaxBytes)
	}
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "quota.mp4")
	if err := os.WriteFile(inputPath, mp4ManySampleFixture(1_000), 0o600); err != nil {
		t.Fatal(err)
	}
	request, err := standard.NewFileJob(inputPath, filepath.Join(directory, "quota-out.mp4"), standard.WithPolicy(policy))
	if err != nil {
		t.Fatal(err)
	}
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := instance.Prepare(t.Context(), request)
	if err == nil {
		closeErr := prepared.Close()
		t.Fatalf("disabled scratch planned a remux that needs a journal (close = %v)", closeErr)
	}
	// The failure has to name the dimension it ran out of, or a caller cannot
	// tell a scratch quota apart from an unsupported graph.
	attributed := false
	for _, item := range diagnostic.ItemsOf(err) {
		attributed = attributed || item.Detail["dimension"] == "scratch"
	}
	if !attributed {
		t.Fatalf("disabled scratch failed without naming the dimension: %v", err)
	}
}

type mp4Reservation struct {
	memory  int64
	claims  resource.Bytes
	scratch plan.Scratch
}

func mp4RemuxReservation(t *testing.T, samples uint32) mp4Reservation {
	t.Helper()
	return mp4RemuxReservationOf(t, mp4ManySampleFixture(samples))
}

func mp4RemuxReservationOf(t *testing.T, source []byte) mp4Reservation {
	t.Helper()
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "samples.mp4")
	outputPath := filepath.Join(directory, "samples-out.mp4")
	if err := os.WriteFile(inputPath, source, 0o600); err != nil {
		t.Fatal(err)
	}
	request, err := standard.NewFileJob(inputPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := instance.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	executed := prepared.Plan()
	result, runErr := prepared.Run(t.Context())
	if runErr != nil || !result.Succeeded() {
		t.Fatalf("%d-byte remux Run = %#v, %v", len(source), result, runErr)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, source) {
		t.Fatalf("%d-byte remux changed the source bytes", len(source))
	}
	var memory int64
	var claims resource.Bytes
	for _, node := range executed.Nodes() {
		memory += int64(node.Estimate.Memory)
		claims += node.Scratch
	}
	return mp4Reservation{memory: memory, claims: claims, scratch: executed.Scratch()}
}
