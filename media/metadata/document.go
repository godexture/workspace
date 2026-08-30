// Package metadata defines the format-independent semantic document that
// carrier payloads parse into and marshal out of.
package metadata

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/plugin"
)

// Scope states what a static document describes. Metadata that changes over
// time is not a document: it is a typed event stream declared with
// schema.Define, so a document never needs a timestamp.
type Scope uint8

const (
	AssetScope Scope = iota + 1
	ProgramScope
	StreamScope
	ChapterScope
)

func (s Scope) Valid() bool { return s >= AssetScope && s <= ChapterScope }

func (s Scope) String() string {
	switch s {
	case AssetScope:
		return "asset"
	case ProgramScope:
		return "program"
	case StreamScope:
		return "stream"
	case ChapterScope:
		return "chapter"
	}
	return "unknown"
}

// BlockID links an entry back to the source block it was parsed from. The
// producing encoding chooses the value; core never interprets it.
type BlockID string

// Origin records where an entry came from, so a re-encode can tell an entry it
// may rewrite in place from one it must convert or report as lost.
type Origin struct {
	// Encoding is the metadata encoding component that produced the entry.
	Encoding plugin.Identity
	// Carrier is the format or bitstream slot the payload was read from.
	Carrier carrier.ID
	// Block links to a source block in the same document.
	Block BlockID
	// Native is the key name in the source encoding, such as an ID3 frame ID.
	Native string
}

// LossOrigin returns this origin only when it identifies a complete source
// block, as required by a loss report.
func (o Origin) LossOrigin() loss.Origin {
	result := loss.Origin{Carrier: o.Carrier, Encoding: o.Encoding.String(), Block: string(o.Block), Native: o.Native}
	if !result.Valid() {
		return loss.Origin{}
	}
	return result
}

// RawBlock records either the source bytes that semantic entries came from or
// an opaque payload that no semantic Origin references. Opaque blocks are only
// safe to carry through an owner that understands their carrier and encoding.
type RawBlock struct {
	id       BlockID
	carrier  carrier.ID
	encoding plugin.Identity
	payload  Blob
	source   bool
}

// NewSourceBlock records parsed source bytes that entries may name in Origin.
func NewSourceBlock(id BlockID, slot carrier.ID, encoding plugin.Identity, payload Blob) RawBlock {
	return RawBlock{id: id, carrier: slot, encoding: encoding, payload: payload, source: true}
}

// NewRawBlock records an opaque payload that an entry Origin cannot reference.
func NewRawBlock(id BlockID, slot carrier.ID, encoding plugin.Identity, payload Blob) RawBlock {
	return RawBlock{id: id, carrier: slot, encoding: encoding, payload: payload}
}

func (b RawBlock) ID() BlockID               { return b.id }
func (b RawBlock) Carrier() carrier.ID       { return b.carrier }
func (b RawBlock) Encoding() plugin.Identity { return b.encoding }
func (b RawBlock) Payload() Blob             { return b.payload }
func (b RawBlock) Source() bool              { return b.source }
func (b RawBlock) Valid() bool {
	return b.id != "" && b.carrier.Valid() && b.payload.Valid() && (!b.source || !b.encoding.IsZero())
}

// Entry is one ordered semantic value. The same key may appear more than once,
// and the order entries were parsed in is preserved.
type Entry struct {
	declaration key.Erased
	value       any
	origin      Origin
}

func (e Entry) Key() key.ID             { return e.declaration.ID() }
func (e Entry) ValueType() reflect.Type { return e.declaration.ValueType() }
func (e Entry) Origin() Origin          { return e.origin }

// Value returns a snapshot of the entry value for surfaces and diagnostics.
// Typed access goes through Key.Values, which avoids the erasure entirely.
func (e Entry) Value() any {
	value, ok := e.declaration.Clone(e.value)
	if !ok {
		return nil
	}
	return value
}

// Document is an immutable ordered set of entries plus source and opaque
// blocks. Entries and Blocks return copies, so a document cannot be
// changed through a slice a caller obtained from it.
type Document struct {
	scope   Scope
	entries []Entry
	blocks  []RawBlock
}

func (d Document) Scope() Scope { return d.scope }
func (d Document) Len() int     { return len(d.entries) }

func (d Document) Entries() []Entry { return append([]Entry(nil), d.entries...) }

func (d Document) Blocks() []RawBlock { return append([]RawBlock(nil), d.blocks...) }

// Block returns one source or opaque block by identity.
func (d Document) Block(id BlockID) (RawBlock, bool) {
	for _, block := range d.blocks {
		if block.id == id {
			return block, true
		}
	}
	return RawBlock{}, false
}

// Edit starts a builder seeded with this document, for changing part of it
// without rebuilding the rest.
func (d Document) Edit() *Builder {
	return &Builder{
		scope:   d.scope,
		entries: append([]Entry(nil), d.entries...),
		blocks:  append([]RawBlock(nil), d.blocks...),
	}
}

// Builder accumulates entries in parse order and reports every problem at
// Build rather than at the first bad value.
type Builder struct {
	scope    Scope
	entries  []Entry
	blocks   []RawBlock
	problems []error
}

// NewBuilder starts an empty document in the given scope.
func NewBuilder(scope Scope) *Builder {
	builder := &Builder{scope: scope}
	if !scope.Valid() {
		builder.problems = append(builder.problems, fmt.Errorf("metadata scope %d is not one of asset, program, stream, chapter", scope))
	}
	return builder
}

// Add appends one typed value. It is a function rather than a method because
// the value type belongs to the key, not to the builder.
func Add[T any](builder *Builder, declaration key.Key[T], value T, origin Origin) *Builder {
	if builder == nil {
		return builder
	}
	return builder.add(declaration.Erased(), value, origin)
}

// AddBlock appends a source or opaque block. Blocks keep their parse order too.
func (b *Builder) AddBlock(block RawBlock) *Builder {
	if b == nil {
		return b
	}
	if !block.Valid() {
		b.problems = append(b.problems, fmt.Errorf("metadata block %q needs an identity and a payload", block.id))
		return b
	}
	for _, existing := range b.blocks {
		if existing.id == block.id {
			b.problems = append(b.problems, fmt.Errorf("metadata block %q is repeated", block.id))
			return b
		}
	}
	b.blocks = append(b.blocks, block)
	return b
}

// Append merges one immutable document without changing entry or block order.
// The zero document represents an absent descriptor attachment and is ignored.
func (b *Builder) Append(document Document) *Builder {
	if b == nil {
		return b
	}
	if document.scope == 0 && len(document.entries) == 0 && len(document.blocks) == 0 {
		return b
	}
	if !document.scope.Valid() || document.scope != b.scope {
		b.problems = append(b.problems, fmt.Errorf("metadata document scope %s cannot be appended to %s", document.scope, b.scope))
		return b
	}
	for _, block := range document.blocks {
		b.AddBlock(block)
	}
	b.entries = append(b.entries, document.entries...)
	return b
}

func (b *Builder) add(declaration key.Erased, value any, origin Origin) *Builder {
	if problem := declaration.Problem(); problem != nil {
		b.problems = append(b.problems, problem)
		return b
	}
	if !declaration.Valid() {
		b.problems = append(b.problems, errors.New("metadata key is not declared"))
		return b
	}
	cloned, ok := declaration.Clone(value)
	if !ok {
		b.problems = append(b.problems, fmt.Errorf("%w: key %s wants %s", key.ErrType, declaration.ID(), declaration.ValueType()))
		return b
	}
	b.entries = append(b.entries, Entry{
		declaration: declaration,
		value:       cloned,
		origin:      origin,
	})
	return b
}

// Build returns the immutable document, or every problem collected so far.
func (b *Builder) Build() (Document, error) {
	if b == nil {
		return Document{}, errors.New("metadata builder is nil")
	}
	problems := append([]error(nil), b.problems...)
	for _, entry := range b.entries {
		origin := entry.origin
		if origin == (Origin{}) {
			continue
		}
		if origin.Block == "" || !origin.Carrier.Valid() || origin.Encoding.IsZero() {
			problems = append(problems, fmt.Errorf("metadata entry %s has an incomplete source origin", entry.Key()))
			continue
		}
		block, ok := b.block(origin.Block)
		if !ok {
			problems = append(problems, fmt.Errorf("metadata entry %s names source block %q, which is not in the document", entry.Key(), origin.Block))
			continue
		}
		if !block.Source() {
			problems = append(problems, fmt.Errorf("metadata entry %s names opaque block %q as its source", entry.Key(), origin.Block))
			continue
		}
		if block.Carrier() != origin.Carrier || block.Encoding() != origin.Encoding {
			problems = append(problems, fmt.Errorf("metadata entry %s names source block %q with incompatible provenance", entry.Key(), origin.Block))
		}
	}
	if len(problems) > 0 {
		return Document{}, errors.Join(problems...)
	}
	return Document{
		scope:   b.scope,
		entries: append([]Entry(nil), b.entries...),
		blocks:  append([]RawBlock(nil), b.blocks...),
	}, nil
}

func (b *Builder) block(id BlockID) (RawBlock, bool) {
	for _, block := range b.blocks {
		if block.id == id {
			return block, true
		}
	}
	return RawBlock{}, false
}
