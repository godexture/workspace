package standard_test

import (
	"context"
	"log"

	"github.com/godexture/godec/standard"
)

func ExampleConvert() {
	if err := standard.Convert(context.Background(), "input.wav", "output.wav"); err != nil {
		log.Fatal(err)
	}
}
