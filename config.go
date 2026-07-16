package pcm

//go:generate go run ../../tools/config-generator -source=internal/encoder.go -type=EncoderConfig -resolved-type=internal.EncoderConfig -default=internal.DefaultEncoderConfig -import=internal=github.com/godexture/codec-pcm/internal -output=config_encoder.go
//go:generate go run ../../tools/config-generator -source=internal/decoder.go -type=DecoderConfig -resolved-type=internal.DecoderConfig -default=internal.DefaultDecoderConfig -import=internal=github.com/godexture/codec-pcm/internal -output=config_decoder.go

func (DecoderConfig) NodeConfiguration() {}
func (EncoderConfig) NodeConfiguration() {}
