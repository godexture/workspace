package job_test

import (
	"fmt"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plugin"
)

type jobExampleSourceID struct{}
type jobExampleSinkID struct{}

// Input and output locations are exclusive tagged choices; their ordinary
// representation keeps reference credentials out of logs and plans.
func ExampleNew() {
	inputReference, _ := access.Parse("file:///input.raw")
	outputReference, _ := access.Parse("file:///output.raw")
	input, _ := job.InputFromReference(inputReference)
	output, _ := job.OutputToReference(outputReference)
	graph, _ := job.NewGraph(
		[]job.Node{
			job.NewNode("source", plugin.IdentityOf[jobExampleSourceID](), config.NewPatch()),
			job.NewNode("sink", plugin.IdentityOf[jobExampleSinkID](), config.NewPatch()),
		},
		[]job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))},
	)
	request, err := job.New([]job.Input{input}, []job.Output{output}, graph)
	if err != nil {
		panic(err)
	}

	fmt.Println(request.Valid())
	fmt.Println(inputReference, outputReference)
	// Output:
	// true
	// file:<redacted> file:<redacted>
}
