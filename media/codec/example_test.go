package codec_test

import (
	"fmt"

	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/format"
)

type codecExampleDecoderID struct{}
type codecExampleEncoderID struct{}
type codecExampleParserID struct{}

// A binding joins a format tag to the components that implement it at
// composition time; neither implementation imports the format. A tag names a
// codec rather than a direction, so each role is stated as what it is.
func ExampleBindDecoder() {
	tag := format.NewTag("wave", "0x0055")
	decoder := codec.BindDecoder(tag, codec.Define[codecExampleDecoderID]())
	encoder := codec.BindEncoder(tag, codec.Define[codecExampleEncoderID]())
	parser := codec.BindParser(tag, codec.DefineParser[codecExampleParserID]())

	for _, binding := range []codec.Binding{decoder, encoder, parser} {
		named, role, _ := codec.BindingTag(binding.Key())
		component, _ := binding.Targets()[0].Component()
		fmt.Println(named, role, component.Name())
	}
	// Output:
	// wave:0x0055 decoder codecExampleDecoderID
	// wave:0x0055 encoder codecExampleEncoderID
	// wave:0x0055 parser codecExampleParserID
}
