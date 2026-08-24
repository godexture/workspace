package metadata_test

import (
	"fmt"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/plugin"
)

type metadataExampleTitleID struct{}
type metadataExampleMoodID struct{}
type metadataExampleGenreID struct{}
type metadataExampleCarrierID struct{}
type metadataExampleEncodingID struct{}

// A document preserves order and repeated open keys. Building a new document
// never mutates a document that has already been handed to another caller.
func ExampleNewBuilder() {
	title := key.Define[metadataExampleTitleID, string]()
	builder := metadata.NewBuilder(metadata.AssetScope)
	metadata.Add(builder, title, "Primary", metadata.Origin{})
	metadata.Add(builder, title, "Alternate", metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		panic(err)
	}

	fmt.Println(document.Scope(), document.Len())
	fmt.Println(metadata.Values(document, title))
	// Output:
	// asset 2
	// [Primary Alternate]
}

// Blob copies bytes once on input and shares immutable backing across later
// values.
func ExampleNewBlob() {
	source := []byte{1, 2, 3}
	blob := metadata.NewBlob("application/octet-stream", source)
	source[0] = 9

	fmt.Println(blob.MediaType(), blob.AppendTo(nil))
	// Output: application/octet-stream [1 2 3]
}

// Bind connects a physical carrier slot to the component that interprets its
// metadata payload without coupling either implementation to the other.
func ExampleBind() {
	slot := carrier.Define[metadataExampleCarrierID]()
	binding := metadata.Bind(slot, plugin.IdentityOf[metadataExampleEncodingID]())
	target, _ := binding.Targets()[0].Component()

	fmt.Println(binding.Key().Name() == slot.String())
	fmt.Println(target.Name())
	// Output:
	// true
	// metadataExampleEncodingID
}

// Map declares conversion direction and loss explicitly; the host never
// infers that two independently authored keys mean the same thing.
func ExampleMap() {
	mood := key.Define[metadataExampleMoodID, string]()
	genre := key.Define[metadataExampleGenreID, string]()
	mapping := metadata.Map(mood, genre, loss.Ambiguous, 10, func(value string) (string, bool) {
		if value == "melancholic" {
			return "Blues", true
		}
		return "", false
	})
	converted, ok := mapping.Convert("melancholic")

	fmt.Println(mapping.Lossiness(), mapping.Priority())
	fmt.Println(converted, ok)
	// Output:
	// ambiguous 10
	// Blues true
}
