package audio

import (
	"context"
	"strconv"
	"testing"

	"github.com/godexture/godec/flow"
	mediaaudio "github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/timing"
)

type quietDomain struct{}

func (quietDomain) Cleanup(error) {}

// capture is the next stage of an execution island: it takes what it is handed
// and holds it for whoever comes after, which is what lets a chain of filters
// be driven one frame at a time without a runtime around it.
type capture struct {
	item flow.Item[mediaaudio.Frame[float32]]
}

func (c *capture) Own(into *flow.Item[mediaaudio.Frame[float32]], value mediaaudio.Frame[float32]) {
	into.Bind(sample.Frames[float32](), quietDomain{})
	into.Set(value)
}

func (c *capture) Emit(context.Context, *flow.Item[mediaaudio.Frame[float32]]) error { return nil }

func (c *capture) take(item *flow.Item[mediaaudio.Frame[float32]]) {
	c.item.Bind(sample.Frames[float32](), quietDomain{})
	c.item.Move(item)
}

// emitting hands the frame on and then keeps it, so a stage can be driven and
// its result read without the runtime that would normally do both.
type emitting struct{ target *capture }

func (e emitting) Own(into *flow.Item[mediaaudio.Frame[float32]], value mediaaudio.Frame[float32]) {
	into.Bind(sample.Frames[float32](), quietDomain{})
	into.Set(value)
}

func (e emitting) Emit(_ context.Context, item *flow.Item[mediaaudio.Frame[float32]]) error {
	e.target.take(item)
	return nil
}

// A chain of filters is one buffer travelling through it. Every stage owns the
// frame alone and edits it where it lies, so what a frame costs does not grow
// with the length of the chain -- which is the whole reason the samples are
// converted at the edges of a region rather than around every stage in it.
func BenchmarkFilterChainCostsPerFrameNotPerFilter(b *testing.B) {
	const samples = 1024
	for _, filters := range []int{1, 4, 16} {
		b.Run(strconv.Itoa(filters)+"-filters", func(b *testing.B) {
			allocator, err := buffer.NewAllocator(1 << 22)
			if err != nil {
				b.Fatal(err)
			}
			chain := make([]*filterOperator, filters)
			captures := make([]capture, filters)
			// The emitters are built once: boxing one per stage per frame would
			// be this harness allocating, not the chain.
			emitters := make([]flow.Emitter[mediaaudio.Frame[float32]], filters)
			for index := range chain {
				chain[index] = newFilterOperator(
					filterPlan[gainConfig]{shape: filterShape(), channels: 1},
					gain{factor: 1.0001}, allocator)
				emitters[index] = emitting{target: &captures[index]}
			}
			ctx := context.Background()

			b.ReportAllocs()
			for b.Loop() {
				current := flow.NewItem(benchmarkFrame(b, allocator, samples), sample.Frames[float32](), quietDomain{})
				for index, stage := range chain {
					if err := stage.Process(ctx, &current, emitters[index]); err != nil {
						b.Fatal(err)
					}
					current.Bind(sample.Frames[float32](), quietDomain{})
					current.Move(&captures[index].item)
				}
				current.Drop()
			}
		})
	}
}

func benchmarkFrame(b *testing.B, allocator *buffer.Allocator, samples int) mediaaudio.Frame[float32] {
	b.Helper()
	handle, err := allocator.Allocate(buffer.Spec{Alignment: 16, Planes: []buffer.PlaneSpec{{Size: samples * 4}}})
	if err != nil {
		b.Fatal(err)
	}
	frame, err := mediaaudio.NewFrame[float32](timing.SomePTS(timing.NewPTS(0)), samples, handle)
	if err != nil {
		b.Fatal(err)
	}
	return frame
}

// The benchmark above reports the cost; this fails when it starts growing.
// What a chain of filters must not do is charge per filter, because that is
// the cost the whole shape of the family exists to remove.
func TestAFilterChainAllocatesTheSameForOneFilterAsForSixteen(t *testing.T) {
	measure := func(filters int) float64 {
		allocator, err := buffer.NewAllocator(1 << 22)
		if err != nil {
			t.Fatal(err)
		}
		chain := make([]*filterOperator, filters)
		captures := make([]capture, filters)
		emitters := make([]flow.Emitter[mediaaudio.Frame[float32]], filters)
		for index := range chain {
			chain[index] = newFilterOperator(
				filterPlan[gainConfig]{shape: filterShape(), channels: 1},
				gain{factor: 1.0001}, allocator)
			emitters[index] = emitting{target: &captures[index]}
		}
		ctx := context.Background()
		return testing.AllocsPerRun(50, func() {
			handle, allocErr := allocator.Allocate(buffer.Spec{Alignment: 16, Planes: []buffer.PlaneSpec{{Size: 4096}}})
			if allocErr != nil {
				t.Fatal(allocErr)
			}
			frame, frameErr := mediaaudio.NewFrame[float32](timing.SomePTS(timing.NewPTS(0)), 1024, handle)
			if frameErr != nil {
				t.Fatal(frameErr)
			}
			current := flow.NewItem(frame, sample.Frames[float32](), quietDomain{})
			for index, stage := range chain {
				if err := stage.Process(ctx, &current, emitters[index]); err != nil {
					t.Fatal(err)
				}
				current.Bind(sample.Frames[float32](), quietDomain{})
				current.Move(&captures[index].item)
			}
			current.Drop()
		})
	}
	one, sixteen := measure(1), measure(16)
	if sixteen != one {
		t.Fatalf("a chain of sixteen filters allocated %v per frame, one allocated %v", sixteen, one)
	}
}
