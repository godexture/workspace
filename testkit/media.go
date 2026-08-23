package testkit

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
)

type streamOptions struct {
	id       stream.ID
	metadata metadata.Document
}

// StreamOption changes descriptor identity or metadata carried by a fixture.
type StreamOption func(*streamOptions)

// WithStreamID selects the stream identity used by the fixture descriptor.
func WithStreamID(id stream.ID) StreamOption { return func(options *streamOptions) { options.id = id } }

// WithMetadata attaches immutable stream metadata to the fixture descriptor.
func WithMetadata(document metadata.Document) StreamOption {
	return func(options *streamOptions) { options.metadata = document }
}

// Chunk is the logical, ownership-free representation of packet.Chunk used
// by typed input fixtures and expectations.
type Chunk struct {
	Sequence uint64
	PTS      timing.OptionalPTS
	DTS      timing.OptionalDTS
	Duration timing.OptionalDuration
	Bytes    []byte
}

// Packet is the logical, ownership-free representation of packet.Packet.
type Packet struct {
	Sequence uint64
	PTS      timing.OptionalPTS
	DTS      timing.OptionalDTS
	Duration timing.OptionalDuration
	Bytes    []byte
}

// Write is the logical positioned-write representation.
type Write struct {
	Operation access.WriteOperation
	Offset    int64
	Bytes     []byte
}

// ByteInput builds grant-independent input bytes. The common runner verifies
// that every retained handle is returned after all scenario shares close.
func ByteInput(parts ...[]byte) Fixture[buffer.Handle] {
	descriptor := stream.MustDescriptor("fixture", access.Bytes().Descriptor(), timing.Base{}, property.New())
	return byteFixture(descriptor, parts)
}

// ByteInputWith builds bytes carrying the requested stream descriptor options.
func ByteInputWith(parts [][]byte, options ...StreamOption) Fixture[buffer.Handle] {
	descriptor := carrierDescriptor(access.Bytes().Descriptor(), options...)
	return byteFixture(descriptor, parts)
}

// ChunkInput builds container chunks for a sample description.
func ChunkInput(description sample.Description, values []Chunk, options ...StreamOption) Fixture[packet.Chunk] {
	descriptor, err := mediaDescriptor(mediaformat.Chunks().Descriptor(), description, options...)
	if err != nil {
		return Fixture[packet.Chunk]{}
	}
	return ChunkInputFor(descriptor, values)
}

// ChunkInputFor builds container chunks for an explicitly supplied
// descriptor. It supports non-audio third-party Formats without inventing PCM
// properties while retaining the same ownership accounting as ChunkInput.
func ChunkInputFor(descriptor stream.Descriptor, values []Chunk) Fixture[packet.Chunk] {
	if !descriptor.Valid() || descriptor.Schema() != mediaformat.Chunks().Identity() {
		return Fixture[packet.Chunk]{}
	}
	allocator, err := buffer.NewAllocator(payloadLimit(chunkPayloads(values)))
	if err != nil {
		return Fixture[packet.Chunk]{}
	}
	items := make([]packet.Chunk, 0, len(values))
	for _, value := range values {
		payload, allocationErr := allocator.FromBytes(value.Bytes, 1)
		if allocationErr != nil {
			releaseChunks(items)
			return Fixture[packet.Chunk]{}
		}
		items = append(items, packet.NewChunk(value.Sequence, value.PTS, value.DTS, value.Duration, payload))
	}
	result := Values(descriptor, mediaformat.Chunks(), items...)
	releaseChunks(items)
	result.verify = allocatorVerifier(allocator)
	return result
}

// PacketInput builds codec packets for a sample description.
func PacketInput(description sample.Description, values []Packet, options ...StreamOption) Fixture[packet.Packet] {
	descriptor, err := mediaDescriptor(codec.Packets().Descriptor(), description, options...)
	if err != nil {
		return Fixture[packet.Packet]{}
	}
	return PacketInputFor(descriptor, values)
}

// PacketInputFor builds codec packets for an explicitly supplied descriptor.
// It keeps packet ownership in testkit without assuming PCM properties.
func PacketInputFor(descriptor stream.Descriptor, values []Packet) Fixture[packet.Packet] {
	if !descriptor.Valid() || descriptor.Schema() != codec.Packets().Identity() {
		return Fixture[packet.Packet]{}
	}
	allocator, err := buffer.NewAllocator(payloadLimit(packetPayloads(values)))
	if err != nil {
		return Fixture[packet.Packet]{}
	}
	items := make([]packet.Packet, 0, len(values))
	for _, value := range values {
		payload, allocationErr := allocator.FromBytes(value.Bytes, 1)
		if allocationErr != nil {
			releasePackets(items)
			return Fixture[packet.Packet]{}
		}
		items = append(items, packet.NewPacket(value.Sequence, value.PTS, value.DTS, value.Duration, payload))
	}
	result := Values(descriptor, codec.Packets(), items...)
	releasePackets(items)
	result.verify = allocatorVerifier(allocator)
	return result
}

// WantBytes compares each emitted byte handle after copying its payload.
func WantBytes(parts ...[]byte) Expectation[buffer.Handle] {
	want := cloneByteSlices(parts)
	return WantValues(want, func(value buffer.Handle) ([]byte, error) {
		if !value.Valid() {
			return nil, errors.New("invalid byte handle")
		}
		return value.Bytes().AppendTo(nil), nil
	})
}

// WantByteStream compares concatenated emitted handles, independent of their
// runtime chunk boundaries.
func WantByteStream(want []byte) Expectation[buffer.Handle] {
	return Expectation[buffer.Handle]{newRecorder: func() recorder[buffer.Handle] {
		return &byteStreamRecorder{want: append([]byte(nil), want...)}
	}}
}

// WantChunks compares logical chunk fields and payloads in order.
func WantChunks(want ...Chunk) Expectation[packet.Chunk] {
	return WantValues(cloneChunks(want), snapshotChunk)
}

// WantPackets compares logical packet fields and payloads in order.
func WantPackets(want ...Packet) Expectation[packet.Packet] {
	return WantValues(clonePackets(want), snapshotPacket)
}

// WantWrites compares positioned writes and payloads in order.
func WantWrites(want ...Write) Expectation[access.Write] {
	return WantValues(cloneWrites(want), snapshotWrite)
}

// WantWriteImage applies Append and Patch operations and compares the final
// byte image. This keeps sink positioning logic inside testkit.
func WantWriteImage(want []byte) Expectation[access.Write] {
	return Expectation[access.Write]{newRecorder: func() recorder[access.Write] {
		return &writeImageRecorder{want: append([]byte(nil), want...)}
	}}
}

func byteFixture(descriptor stream.Descriptor, parts [][]byte) Fixture[buffer.Handle] {
	allocator, err := buffer.NewAllocator(payloadLimit(parts))
	if err != nil {
		return Fixture[buffer.Handle]{}
	}
	handles := make([]buffer.Handle, 0, len(parts))
	for _, part := range parts {
		handle, allocationErr := allocator.FromBytes(part, 1)
		if allocationErr != nil {
			releaseHandles(handles)
			return Fixture[buffer.Handle]{}
		}
		handles = append(handles, handle)
	}
	result := Values(descriptor, access.Bytes(), handles...)
	releaseHandles(handles)
	result.verify = allocatorVerifier(allocator)
	return result
}

func carrierDescriptor(schemaDescriptor schema.Descriptor, options ...StreamOption) stream.Descriptor {
	state := applyStreamOptions(options)
	descriptor := stream.MustDescriptor(state.id, schemaDescriptor, timing.Base{}, property.New())
	return descriptor.WithMetadata(state.metadata)
}

func mediaDescriptor(schemaDescriptor schema.Descriptor, description sample.Description, options ...StreamOption) (stream.Descriptor, error) {
	if !schemaDescriptor.Valid() || !schemaDescriptor.HasTime() {
		return stream.Descriptor{}, errors.New("fixture schema identity is invalid")
	}
	properties, err := description.Properties()
	if err != nil {
		return stream.Descriptor{}, err
	}
	state := applyStreamOptions(options)
	descriptor, err := stream.NewDescriptor(state.id, schemaDescriptor, timing.MustBase(1, int64(description.Rate)), properties)
	if err != nil {
		return stream.Descriptor{}, err
	}
	return descriptor.WithMetadata(state.metadata), nil
}

func applyStreamOptions(values []StreamOption) streamOptions {
	result := streamOptions{id: "fixture"}
	for _, option := range values {
		if option != nil {
			option(&result)
		}
	}
	if result.id.IsZero() {
		result.id = "fixture"
	}
	return result
}

func payloadLimit(parts [][]byte) int64 {
	var total int64
	for _, part := range parts {
		size := len(part)
		if size == 0 {
			size = 1
		}
		total += int64(size)
	}
	if total == 0 {
		return 1
	}
	return total
}

func chunkPayloads(values []Chunk) [][]byte {
	result := make([][]byte, len(values))
	for index := range values {
		result[index] = values[index].Bytes
	}
	return result
}

func packetPayloads(values []Packet) [][]byte {
	result := make([][]byte, len(values))
	for index := range values {
		result[index] = values[index].Bytes
	}
	return result
}

func snapshotChunk(value packet.Chunk) (Chunk, error) {
	if !value.Valid() {
		return Chunk{}, errors.New("invalid chunk")
	}
	return Chunk{Sequence: value.Sequence(), PTS: value.PTS(), DTS: value.DTS(), Duration: value.Duration(), Bytes: value.Bytes().AppendTo(nil)}, nil
}

func snapshotPacket(value packet.Packet) (Packet, error) {
	if !value.Valid() {
		return Packet{}, errors.New("invalid packet")
	}
	return Packet{
		Sequence: value.Sequence(), PTS: value.PTS(), DTS: value.DTS(), Duration: value.Duration(),
		Bytes: value.Bytes().AppendTo(nil),
	}, nil
}

func snapshotWrite(value access.Write) (Write, error) {
	if !value.Valid() {
		return Write{}, errors.New("invalid positioned write")
	}
	return Write{Operation: value.Operation(), Offset: value.Offset(), Bytes: value.Bytes().AppendTo(nil)}, nil
}

type byteStreamRecorder struct {
	mu   sync.Mutex
	want []byte
	got  []byte
	err  error
}

func (r *byteStreamRecorder) accept(value buffer.Handle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !value.Valid() {
		r.err = errors.Join(r.err, errors.New("invalid byte handle"))
		return
	}
	r.got = value.Bytes().AppendTo(r.got)
}

func (r *byteStreamRecorder) finish() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	if !bytes.Equal(r.got, r.want) {
		return fmt.Errorf("byte stream mismatch: got %x, want %x", r.got, r.want)
	}
	return nil
}

type writeImageRecorder struct {
	mu   sync.Mutex
	want []byte
	got  []byte
	err  error
}

func (r *writeImageRecorder) accept(value access.Write) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !value.Valid() {
		r.err = errors.Join(r.err, errors.New("invalid positioned write"))
		return
	}
	payload := value.Bytes()
	switch value.Operation() {
	case access.AppendOperation:
		r.got = payload.AppendTo(r.got)
	case access.PatchOperation:
		end := value.Offset() + int64(payload.Len())
		if value.Offset() < 0 || end < value.Offset() || end > int64(^uint(0)>>1) {
			r.err = errors.Join(r.err, errors.New("positioned write range is invalid"))
			return
		}
		if end > int64(len(r.got)) {
			r.got = append(r.got, make([]byte, int(end)-len(r.got))...)
		}
		payload.CopyTo(r.got[int(value.Offset()):int(end)])
	default:
		r.err = errors.Join(r.err, errors.New("unknown positioned write operation"))
	}
}

func (r *writeImageRecorder) finish() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	if !bytes.Equal(r.got, r.want) {
		return fmt.Errorf("positioned output mismatch: got %x, want %x", r.got, r.want)
	}
	return nil
}

func allocatorVerifier(allocator *buffer.Allocator) func() error {
	return func() error {
		if used := allocator.Used(); used != 0 {
			return fmt.Errorf("fixture payload allocator retained %d bytes", used)
		}
		return nil
	}
}

func cloneByteSlices(values [][]byte) [][]byte {
	result := make([][]byte, len(values))
	for index, value := range values {
		result[index] = append([]byte(nil), value...)
	}
	return result
}

func cloneChunks(values []Chunk) []Chunk {
	result := append([]Chunk(nil), values...)
	for index := range result {
		result[index].Bytes = append([]byte(nil), result[index].Bytes...)
	}
	return result
}

func clonePackets(values []Packet) []Packet {
	result := append([]Packet(nil), values...)
	for index := range result {
		result[index].Bytes = append([]byte(nil), result[index].Bytes...)
	}
	return result
}

func cloneWrites(values []Write) []Write {
	result := append([]Write(nil), values...)
	for index := range result {
		result[index].Bytes = append([]byte(nil), result[index].Bytes...)
	}
	return result
}

func releaseHandles(values []buffer.Handle) {
	for _, value := range values {
		value.Release()
	}
}

func releaseChunks(values []packet.Chunk) {
	for _, value := range values {
		value.Release()
	}
}

func releasePackets(values []packet.Packet) {
	for _, value := range values {
		value.Release()
	}
}
