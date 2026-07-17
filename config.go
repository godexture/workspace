package mp3

//go:generate go run ../../tools/config-generator -source=internal/config.go -type=DemuxerConfig -resolved-type=internal.DemuxerConfig -import=internal=github.com/godexture/format-mp3/internal -output=config_demuxer.go
//go:generate go run ../../tools/config-generator -source=internal/config.go -type=MuxerConfig -resolved-type=internal.MuxerConfig -import=internal=github.com/godexture/format-mp3/internal -output=config_muxer.go

func (DemuxerConfig) NodeConfiguration() {}
func (MuxerConfig) NodeConfiguration()   {}
