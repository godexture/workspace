package host

import (
	"context"
	"errors"
	"fmt"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/resource"
)

// inspectLimit is the Host-owned byte budget for one Format Inspect. Enforcing
// it in a session wrapper rather than trusting the Format keeps a hostile or
// buggy container header from reading a source without bound, and it measures
// the exact usage that reaches the Plan.
type inspectLimit struct {
	session access.Session
	limit   resource.Bytes
	used    resource.Bytes
}

func newInspectLimit(session access.Session, limit resource.Bytes) *inspectLimit {
	return &inspectLimit{session: session, limit: limit}
}

func (l *inspectLimit) Capabilities() access.Capabilities { return l.session.Capabilities() }

// Close is a no-op because Prepare keeps the underlying session for the run.
func (*inspectLimit) Close() error { return nil }

func (l *inspectLimit) Remaining() resource.Bytes {
	if l.used >= l.limit {
		return 0
	}
	return l.limit - l.used
}

func (l *inspectLimit) Used() resource.Bytes { return l.used }

func (l *inspectLimit) charge(count int) error {
	if count <= 0 {
		return nil
	}
	if resource.Bytes(count) > l.Remaining() {
		l.used = l.limit
		return fmt.Errorf("%w: Format Inspect read past %d bytes", errInspectBudget, l.limit)
	}
	l.used += resource.Bytes(count)
	return nil
}

func (l *inspectLimit) Read(ctx context.Context, destination []byte) (int, error) {
	reader, ok := l.session.(access.Sequential)
	if !ok {
		return 0, access.ErrCapabilityView
	}
	if err := l.charge(len(destination)); err != nil {
		return 0, err
	}
	return reader.Read(ctx, destination)
}

func (l *inspectLimit) ReadAt(ctx context.Context, destination []byte, offset int64) (int, error) {
	reader, ok := l.session.(access.Random)
	if !ok {
		return 0, access.ErrCapabilityView
	}
	if err := l.charge(len(destination)); err != nil {
		return 0, err
	}
	return reader.ReadAt(ctx, destination, offset)
}

func (l *inspectLimit) Size(ctx context.Context) (int64, error) {
	sizer, ok := l.session.(access.Sizer)
	if !ok {
		return 0, access.ErrCapabilityView
	}
	return sizer.Size(ctx)
}

var errInspectBudget = errors.New("Format Inspect budget was exhausted")
