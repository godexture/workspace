package acme

import "github.com/godexture/godec/media/schema"

type valueSchemaID struct{}

// Value is the decoded item carried by the fixture's open schema.
type Value struct{ Number byte }

var values = schema.Define[valueSchemaID](schema.Traits[Value]{
	Size: func(Value) int { return 1 },
})

func Values() schema.Type[Value] { return values }
