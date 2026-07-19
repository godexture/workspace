//go:generate go run ../../tools/config-generator -source=internal/domain/config.go -type=EncoderConfig -resolved-type=domain.EncoderConfig -default=domain.DefaultEncoderConfig -import=domain=github.com/godexture/codec-mp3/internal/domain -output=config_encoder.go
//go:generate go run ../../tools/config-generator -source=internal/domain/config.go -type=DecoderConfig -resolved-type=domain.DecoderConfig -default=domain.DefaultDecoderConfig -import=domain=github.com/godexture/codec-mp3/internal/domain -output=config_decoder.go

package mp3
