package media_test

import (
	"reflect"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/packet"
)

func TestPublicMediaReadsReturnImmutableViews(t *testing.T) {
	byteView := reflect.TypeFor[buffer.Bytes]()
	sampleView := reflect.TypeFor[audio.Samples[int16]]()
	methods := []struct {
		typeOf reflect.Type
		name   string
		want   reflect.Type
	}{
		{reflect.TypeFor[buffer.Handle](), "Bytes", byteView},
		{reflect.TypeFor[buffer.Handle](), "Plane", byteView},
		{reflect.TypeFor[buffer.View](), "Bytes", byteView},
		{reflect.TypeFor[buffer.View](), "Plane", byteView},
		{reflect.TypeFor[packet.Chunk](), "Bytes", byteView},
		{reflect.TypeFor[packet.Packet](), "Bytes", byteView},
		{reflect.TypeFor[access.Write](), "Bytes", byteView},
		{reflect.TypeFor[audio.Frame[int16]](), "Plane", byteView},
		{reflect.TypeFor[audio.Frame[int16]](), "PlaneSamples", sampleView},
	}
	for _, test := range methods {
		method, ok := test.typeOf.MethodByName(test.name)
		if !ok {
			t.Fatalf("%s.%s is missing", test.typeOf, test.name)
		}
		if got := method.Type.Out(0); got != test.want {
			t.Errorf("%s.%s returns %s, want %s", test.typeOf, test.name, got, test.want)
		}
	}
	if _, ok := reflect.TypeFor[buffer.Handle]().MethodByName("MutableBytes"); ok {
		t.Fatal("buffer.Handle exposes direct mutable backing")
	}
	assertPrivateBacking(t, byteView)
	assertPrivateBacking(t, sampleView)
}

func assertPrivateBacking(t *testing.T, typeOf reflect.Type) {
	t.Helper()
	for index := range typeOf.NumField() {
		if field := typeOf.Field(index); field.IsExported() {
			t.Errorf("%s.%s exposes immutable view backing", typeOf, field.Name)
		}
	}
}
