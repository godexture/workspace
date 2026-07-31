package manifest

type NodeType string

const (
	RoleDemuxer NodeType = "demuxer"
	RoleMuxer   NodeType = "muxer"
	RoleDecoder NodeType = "decoder"
	RoleEncoder NodeType = "encoder"
	RoleFilter  NodeType = "filter"
	RoleSink    NodeType = "sink"
	RoleUnknown NodeType = "unknown"
)
