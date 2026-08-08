package codec_test

import (
	"fmt"

	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/format"
)

type codecExampleDecoderID struct{}
type codecExampleParserID struct{}

// A binding joins a format tag to codec and parser component identities at
// composition time; neither implementation imports the format.
func ExampleBind() {
	binding := codec.Bind(
		format.NewTag("wave", "0x0055"),
		codec.Define[codecExampleDecoderID](),
		codec.DefineParser[codecExampleParserID](),
	)
	targets := binding.Targets()
	decoder, _ := targets[0].Component()
	parser, _ := targets[1].Component()

	fmt.Println(binding.Valid(), binding.Key().Name())
	fmt.Println(decoder.Name(), parser.Name())
	// Output:
	// true wave:0x0055
	// codecExampleDecoderID codecExampleParserID
}
