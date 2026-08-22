package sample

import "testing"

func TestLayoutCodecRoundTripsEveryLayoutKind(t *testing.T) {
	codec := LayoutCodec()
	if !codec.Valid() {
		t.Fatal("layout codec is not schema-registrable")
	}
	for _, value := range []Layout{Mono(), Stereo(), Channels(6), Positions(FrontLeft, FrontRight, LowFrequency)} {
		decoded, err := codec.Decode(codec.Encode(value))
		if err != nil || decoded != value {
			t.Errorf("%s round trip = %#v, %v", value, decoded, err)
		}
		canonical, err := codec.Canonical(value)
		if err != nil || len(canonical) == 0 {
			t.Errorf("%s canonical = %q, %v", value, canonical, err)
		}
	}
	if _, err := codec.Decode("surround"); err == nil {
		t.Error("an unparseable layout was accepted")
	}
}

func TestCodingAndEndianCodecsCoverTheVocabulary(t *testing.T) {
	coding := CodingCodec()
	for _, value := range []Coding{U8, S8, S16, S24, S32, F32, F64} {
		decoded, err := coding.Decode(string(value))
		if err != nil || decoded != value {
			t.Errorf("coding %s = %v, %v", value, decoded, err)
		}
	}
	if _, err := coding.Decode("s20"); err == nil {
		t.Error("an unknown coding was accepted")
	}
	endian := EndianCodec()
	for _, value := range []Endian{LittleEndian, BigEndian} {
		decoded, err := endian.Decode(string(value))
		if err != nil || decoded != value {
			t.Errorf("endian %s = %v, %v", value, decoded, err)
		}
	}
	if _, err := endian.Decode(string(NoEndian)); err == nil {
		t.Error("NoEndian was offered as an operator choice")
	}
}
