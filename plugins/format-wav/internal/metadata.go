package internal

import (
	"bytes"
	"encoding/binary"

	"github.com/godexture/core/domain/metadata"
	id3 "github.com/godexture/metadata-id3"
	"github.com/godexture/metadata-id3/id3v2"
	"github.com/godexture/sdk/date"
)

func mapWavInfoTag(meta *metadata.Bundle, tag string, value string) {
	if value == "" {
		return
	}
	switch tag {
	case wavInfoTagTitle:
		meta.Set(metadata.KeyTitle(value))
	case wavInfoTagArtist:
		meta.PushBack(metadata.KeyArtist(value))
	case wavInfoTagDate:
		if d, err := date.NewPartial(value); err == nil {
			meta.Set(metadata.KeyDate(d))
		}
	case wavInfoTagComment:
		meta.Set(metadata.KeyComment(value))
	case wavInfoTagGenre:
		meta.Set(metadata.KeyGenre(value))
	case wavInfoTagAlbum:
		meta.Set(metadata.KeyAlbum(value))
	case wavInfoTagEncoder:
		meta.Set(metadata.KeyEncoder(value))
	case wavInfoTagCopyright:
		meta.Set(metadata.KeyCopyright(value))
	}
}

func buildChunk(id string, payload []byte) []byte {
	size := len(payload)
	pad := size % 2
	buf := make([]byte, 8+size+pad)
	copy(buf[0:4], id)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(size))
	copy(buf[8:8+size], payload)
	if pad == 1 {
		buf[8+size] = 0
	}
	return buf
}

func buildInfoSubchunk(id string, val string) []byte {
	payload := append([]byte(val), 0)
	return buildChunk(id, payload)
}

func buildListInfoChunk(meta metadata.Bundle) []byte {
	var subchunks [][]byte

	if v := metadata.Get[metadata.KeyTitle](&meta); v != "" {
		subchunks = append(subchunks, buildInfoSubchunk(wavInfoTagTitle, string(v)))
	}
	artists := metadata.Enumerate[metadata.KeyArtist](&meta)
	for _, a := range artists {
		subchunks = append(subchunks, buildInfoSubchunk(wavInfoTagArtist, string(a)))
	}
	if v := metadata.Get[metadata.KeyDate](&meta); date.Partial(v).HasValue() {
		subchunks = append(subchunks, buildInfoSubchunk(wavInfoTagDate, date.Partial(v).ToISOString()))
	}
	if v := metadata.Get[metadata.KeyComment](&meta); v != "" {
		subchunks = append(subchunks, buildInfoSubchunk(wavInfoTagComment, string(v)))
	}
	if v := metadata.Get[metadata.KeyGenre](&meta); v != "" {
		subchunks = append(subchunks, buildInfoSubchunk(wavInfoTagGenre, string(v)))
	}
	if v := metadata.Get[metadata.KeyAlbum](&meta); v != "" {
		subchunks = append(subchunks, buildInfoSubchunk(wavInfoTagAlbum, string(v)))
	}
	if v := metadata.Get[metadata.KeyEncoder](&meta); v != "" {
		subchunks = append(subchunks, buildInfoSubchunk(wavInfoTagEncoder, string(v)))
	}
	if v := metadata.Get[metadata.KeyCopyright](&meta); v != "" {
		subchunks = append(subchunks, buildInfoSubchunk(wavInfoTagCopyright, string(v)))
	}

	if len(subchunks) == 0 {
		return nil
	}

	var payload bytes.Buffer
	payload.WriteString(wavTagINFO)
	for _, sc := range subchunks {
		payload.Write(sc)
	}

	return buildChunk(wavTagLIST, payload.Bytes())
}

func buildID3Chunk(meta metadata.Bundle) []byte {
	tag, err := id3v2.Marshal(meta, id3v2.MarshalOptions{Version: id3v2.Version3})
	if err != nil || len(tag) == 0 {
		return nil
	}
	return buildChunk(wavTagID3, tag)
}

func buildRawChunk(id string, meta metadata.Bundle) []byte {
	raw, exists := meta.GetRaw(id)
	if !exists || len(raw) == 0 {
		return nil
	}
	return buildChunk(id, raw[0])
}

func parseAndMergeID3(meta *metadata.Bundle, payload []byte) {
	parsedMeta, err := id3.ParseReader(bytes.NewReader(payload))
	if err == nil && parsedMeta != nil {
		meta.Merge(parsedMeta)
	}
}
