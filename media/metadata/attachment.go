package metadata

import "errors"

var (
	// ErrMetadataAbsent says that no metadata attachment was supplied.
	ErrMetadataAbsent = errors.New("metadata is absent")
	// ErrMetadataUnavailable says that a carrier was present but its semantic
	// document could not be made available.
	ErrMetadataUnavailable = errors.New("metadata is unavailable")
	// ErrMetadataScopeMismatch says that two attachments cannot be merged
	// because they describe different static scopes.
	ErrMetadataScopeMismatch = errors.New("metadata attachment scopes differ")
	// ErrInvalidAttachment identifies a value that does not satisfy the
	// attachment state invariants.
	ErrInvalidAttachment = errors.New("metadata attachment is invalid")
)

// Availability is the state of a stream's static metadata attachment.
type Availability uint8

const (
	AttachmentAbsent Availability = iota
	AttachmentAvailable
	AttachmentUnavailable
)

func (a Availability) Valid() bool { return a <= AttachmentUnavailable }

func (a Availability) String() string {
	switch a {
	case AttachmentAbsent:
		return "absent"
	case AttachmentAvailable:
		return "available"
	case AttachmentUnavailable:
		return "unavailable"
	}
	return "unknown"
}

// Attachment carries one immutable metadata state. The zero value is the
// intentional Absent state. Ordinary semantic consumers must use Semantic and
// receive ErrMetadataUnavailable for an Unavailable attachment.
type Attachment struct {
	state    Availability
	scope    Scope
	document Document
}

// NewAvailable wraps a valid semantic document.
func NewAvailable(document Document) (Attachment, error) {
	if !document.Valid() {
		return Attachment{}, ErrInvalidAttachment
	}
	return Attachment{state: AttachmentAvailable, scope: document.Scope(), document: document}, nil
}

func MustAvailable(document Document) Attachment {
	value, err := NewAvailable(document)
	if err != nil {
		panic(err)
	}
	return value
}

// NewUnavailable records a valid scope without exposing a partial document.
func NewUnavailable(scope Scope) (Attachment, error) {
	if !scope.Valid() {
		return Attachment{}, ErrInvalidAttachment
	}
	return Attachment{state: AttachmentUnavailable, scope: scope}, nil
}

func MustUnavailable(scope Scope) Attachment {
	value, err := NewUnavailable(scope)
	if err != nil {
		panic(err)
	}
	return value
}

// Absent returns the zero metadata attachment explicitly.
func Absent() Attachment { return Attachment{} }

func (a Attachment) Valid() bool {
	switch a.state {
	case AttachmentAbsent:
		return a.scope == 0 && !a.document.Valid()
	case AttachmentAvailable:
		return a.document.Valid() && a.scope == a.document.Scope()
	case AttachmentUnavailable:
		return a.scope.Valid() && !a.document.Valid()
	default:
		return false
	}
}

func (a Attachment) State() Availability { return a.state }
func (a Attachment) Scope() Scope {
	if a.state == AttachmentAvailable {
		return a.document.Scope()
	}
	return a.scope
}
func (a Attachment) IsAbsent() bool      { return a.state == AttachmentAbsent && a.Valid() }
func (a Attachment) IsAvailable() bool   { return a.state == AttachmentAvailable && a.Valid() }
func (a Attachment) IsUnavailable() bool { return a.state == AttachmentUnavailable && a.Valid() }

// Semantic returns the complete document only for Available attachments.
func (a Attachment) Semantic() (Document, error) {
	if !a.Valid() {
		return Document{}, ErrInvalidAttachment
	}
	switch a.state {
	case AttachmentAvailable:
		return a.document, nil
	case AttachmentAbsent:
		return Document{}, ErrMetadataAbsent
	case AttachmentUnavailable:
		return Document{}, ErrMetadataUnavailable
	default:
		return Document{}, ErrInvalidAttachment
	}
}

// SameState compares the availability and scope facts carried in planning
// state. Semantic values are deliberately not part of this comparison; stream
// fingerprints likewise never encode arbitrary metadata values.
func (a Attachment) SameState(other Attachment) bool {
	return a.Valid() && other.Valid() && a.state == other.state && a.Scope() == other.Scope()
}

// Merge combines attachments in document order under an explicit expected
// scope. Absent contributes no document; if any value is Unavailable, the
// merged state remains Unavailable. Repeated references to one immutable
// Document contribute once, while separately built Documents remain distinct.
func Merge(scope Scope, values ...Attachment) (Attachment, error) {
	if !scope.Valid() {
		return Attachment{}, ErrInvalidAttachment
	}
	unavailable := false
	for _, value := range values {
		if !value.Valid() {
			return Attachment{}, ErrInvalidAttachment
		}
		if value.IsAbsent() {
			continue
		}
		if value.Scope() != scope {
			return Attachment{}, ErrMetadataScopeMismatch
		}
		if value.IsUnavailable() {
			unavailable = true
		}
	}
	if unavailable {
		return NewUnavailable(scope)
	}
	var merged Document
	mergedOK := false
	seen := make(map[*documentIdentity]struct{}, len(values))
	for _, value := range values {
		var document Document
		var ok bool
		if value.IsAvailable() {
			document, ok = value.document, true
		}
		if !ok {
			continue
		}
		if _, exists := seen[document.identity]; exists {
			continue
		}
		seen[document.identity] = struct{}{}
		if !mergedOK {
			merged, mergedOK = document, true
			continue
		}
		var err error
		merged, err = NewBuilder(scope).Append(merged).Append(document).Build()
		if err != nil {
			return Attachment{}, err
		}
	}
	if mergedOK {
		return NewAvailable(merged)
	}
	return Absent(), nil
}
