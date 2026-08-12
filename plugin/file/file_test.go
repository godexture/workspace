package file

import (
	"bytes"
	"context"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/plugin"
)

type shortSession struct {
	data  []byte
	limit int
}

type recordingSinkSession struct {
	data  []byte
	limit int
	calls int
}

func (*recordingSinkSession) Capabilities() access.Capabilities {
	capabilities, _ := access.NewCapabilities(access.RandomWrite)
	return capabilities
}

func (*recordingSinkSession) Close() error                        { return nil }
func (*recordingSinkSession) Flush(context.Context) error         { return nil }
func (*recordingSinkSession) Sync(context.Context) error          { return nil }
func (*recordingSinkSession) PrepareCommit(context.Context) error { return nil }
func (*recordingSinkSession) Commit(context.Context) error        { return nil }
func (*recordingSinkSession) Abort(context.Context) error         { return nil }

func (s *recordingSinkSession) WriteAt(_ context.Context, source []byte, offset int64) (int, error) {
	s.calls++
	size := min(len(source), s.limit)
	end := int(offset) + size
	if end > len(s.data) {
		s.data = append(s.data, make([]byte, end-len(s.data))...)
	}
	copy(s.data[int(offset):end], source[:size])
	return size, nil
}

func (*shortSession) Capabilities() access.Capabilities {
	capabilities, _ := access.NewCapabilities(access.SequentialRead)
	return capabilities
}

func (*shortSession) Close() error { return nil }

func (s *shortSession) Read(_ context.Context, destination []byte) (int, error) {
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	size := min(len(destination), len(s.data), s.limit)
	copy(destination, s.data[:size])
	s.data = s.data[size:]
	return size, nil
}

func TestPluginComposesBothFileDirections(t *testing.T) {
	definition := Plugin()
	if len(definition.Components()) != 2 {
		t.Fatalf("file component count = %d", len(definition.Components()))
	}
	if _, err := host.New(host.Plugins(plugin.NewSet(definition))); err != nil {
		t.Fatal(err)
	}
	var sourceFound, sinkFound bool
	for _, component := range definition.Components() {
		if trait, ok := access.SourceOf(component); ok {
			sourceFound = component.Identity() == SourceIdentity() && trait.Scheme() == "file" && trait.Capabilities().Contains(access.CancelableRead)
		}
		if trait, ok := access.SinkOf(component); ok {
			sinkFound = component.Identity() == SinkIdentity() && trait.Scheme() == "file" && trait.TransactionClass() == access.AtomicReplace
		}
	}
	if !sourceFound || !sinkFound {
		t.Fatalf("file traits = source %v, sink %v", sourceFound, sinkFound)
	}
}

func TestSourceFillsBlocksAcrossShortReadsAndReallocatesOnlyTail(t *testing.T) {
	data := make([]byte, blockSize+137)
	for index := range data {
		data[index] = byte(index * 31)
	}
	session := &shortSession{data: append([]byte(nil), data...), limit: 997}
	opening, err := access.NewOpening(access.SourceDirection, session, selection(t, session.Capabilities(), access.SequentialRead), 0)
	if err != nil {
		t.Fatal(err)
	}
	allocator, err := buffer.NewAllocator(3 * blockSize)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := openSource(sourceShape(), opening, allocator)
	if err != nil {
		t.Fatal(err)
	}
	reader := opened.(*sourceOperator)

	first, err := reader.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := reader.Read(context.Background())
	if err != nil {
		first.Drop()
		t.Fatal(err)
	}
	if got := first.Value().Layout().Size; got != blockSize {
		t.Fatalf("first block size = %d", got)
	}
	if got := second.Value().Layout().Size; got != 137 {
		t.Fatalf("tail size = %d", got)
	}
	joined := append(append([]byte(nil), first.Value().Bytes()...), second.Value().Bytes()...)
	if !bytes.Equal(joined, data) {
		t.Fatal("file source changed byte order or content")
	}
	first.Drop()
	second.Drop()
	if _, err := reader.Read(context.Background()); err != io.EOF {
		t.Fatalf("final read error = %v", err)
	}
	if allocator.Used() != 0 {
		t.Fatalf("source allocator retained %d bytes", allocator.Used())
	}
}

func TestSinkReplacesOnlyAtCommitAndReturnsPayloadGrant(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "output.raw")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	reference := fileReference(t, target)
	session, err := acquireSink(context.Background(), reference, selection(t, sinkCapabilities(), access.SequentialWrite))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	opening, err := access.NewOpening(access.SinkDirection, session, selection(t, sinkCapabilities(), access.SequentialWrite), access.AtomicReplace)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := openSink(sinkShape(), opening)
	if err != nil {
		t.Fatal(err)
	}
	operator := opened.(*sinkOperator)
	allocator, err := buffer.NewAllocator(16)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := allocator.FromBytes([]byte("replacement"), 1)
	if err != nil {
		t.Fatal(err)
	}
	write, err := access.Append(handle)
	if err != nil {
		handle.Release()
		t.Fatal(err)
	}
	if err := operator.Write(context.Background(), flow.NewInput(write, access.Writes())); err != nil {
		t.Fatal(err)
	}
	if allocator.Used() != 0 {
		t.Fatalf("sink retained %d payload bytes", allocator.Used())
	}
	assertFile(t, target, []byte("old"))
	if matches := temporaryFiles(t, target); len(matches) != 1 {
		t.Fatalf("temporary files before commit = %v", matches)
	}
	if err := operator.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := operator.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := operator.PrepareCommit(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertFile(t, target, []byte("old"))
	if err := operator.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertFile(t, target, []byte("replacement"))
	if matches := temporaryFiles(t, target); len(matches) != 0 {
		t.Fatalf("temporary files after commit = %v", matches)
	}
}

func TestSinkDispatchesPartialAppendAndAbsolutePatchThroughRandomView(t *testing.T) {
	session := &recordingSinkSession{limit: 2}
	selection := selection(t, session.Capabilities(), access.RandomWrite)
	opening, err := access.NewOpening(access.SinkDirection, session, selection, access.AtomicReplace)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := openSink(sinkShape(), opening)
	if err != nil {
		t.Fatal(err)
	}
	operator := opened.(*sinkOperator)
	allocator, err := buffer.NewAllocator(16)
	if err != nil {
		t.Fatal(err)
	}

	appendPayload, err := allocator.FromBytes([]byte("abcdef"), 1)
	if err != nil {
		t.Fatal(err)
	}
	appendWrite, err := access.Append(appendPayload)
	if err != nil {
		appendPayload.Release()
		t.Fatal(err)
	}
	if err := operator.Write(context.Background(), flow.NewInput(appendWrite, access.Writes())); err != nil {
		t.Fatal(err)
	}

	patchPayload, err := allocator.FromBytes([]byte("XY"), 1)
	if err != nil {
		t.Fatal(err)
	}
	patchWrite, err := access.Patch(2, patchPayload)
	if err != nil {
		patchPayload.Release()
		t.Fatal(err)
	}
	if err := operator.Write(context.Background(), flow.NewInput(patchWrite, access.Writes())); err != nil {
		t.Fatal(err)
	}
	if got := string(session.data); got != "abXYef" {
		t.Fatalf("positioned sink bytes = %q", got)
	}
	if session.calls != 4 {
		t.Fatalf("partial write calls = %d, want 4", session.calls)
	}
	if allocator.Used() != 0 {
		t.Fatalf("positioned sink retained %d payload bytes", allocator.Used())
	}
}

func TestSinkRejectsOverflowWithoutConsumingPayload(t *testing.T) {
	session := &recordingSinkSession{limit: 1}
	selection := selection(t, session.Capabilities(), access.RandomWrite)
	opening, err := access.NewOpening(access.SinkDirection, session, selection, access.AtomicReplace)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := openSink(sinkShape(), opening)
	if err != nil {
		t.Fatal(err)
	}
	allocator, err := buffer.NewAllocator(1)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := allocator.FromBytes([]byte{1}, 1)
	if err != nil {
		t.Fatal(err)
	}
	write, err := access.Patch(math.MaxInt64, payload)
	if err != nil {
		payload.Release()
		t.Fatal(err)
	}
	input := flow.NewInput(write, access.Writes())
	if err := opened.(*sinkOperator).Write(context.Background(), input); err == nil {
		input.Drop()
		t.Fatal("overflowing patch was accepted")
	}
	if allocator.Used() == 0 || session.calls != 0 {
		input.Drop()
		t.Fatalf("failed write ownership/calls = %d/%d", allocator.Used(), session.calls)
	}
	input.Drop()
	if allocator.Used() != 0 {
		t.Fatalf("caller release retained %d payload bytes", allocator.Used())
	}
}

func TestSinkAbortAndFailedCommitLeaveNoTemporaryFile(t *testing.T) {
	t.Run("abort", func(t *testing.T) {
		directory := t.TempDir()
		target := filepath.Join(directory, "output.raw")
		if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		session, operator := openedSink(t, target)
		writeBytes(t, operator, []byte("new"))
		if err := operator.Abort(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := session.Close(); err != nil {
			t.Fatal(err)
		}
		assertFile(t, target, []byte("old"))
		if matches := temporaryFiles(t, target); len(matches) != 0 {
			t.Fatalf("temporary files after abort = %v", matches)
		}
	})

	t.Run("commit failure", func(t *testing.T) {
		directory := t.TempDir()
		target := filepath.Join(directory, "existing-directory")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(target, "keep"), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		session, operator := openedSink(t, target)
		writeBytes(t, operator, []byte("new"))
		if err := operator.PrepareCommit(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := operator.Commit(context.Background()); err == nil {
			t.Fatal("commit unexpectedly replaced a non-empty directory")
		}
		if err := operator.Abort(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := session.Close(); err != nil {
			t.Fatal(err)
		}
		assertFile(t, filepath.Join(target, "keep"), []byte("old"))
		if matches := temporaryFiles(t, target); len(matches) != 0 {
			t.Fatalf("temporary files after failed commit = %v", matches)
		}
	})
}

func TestFileReferenceResolvesEscapedAbsolutePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "space % value.raw")
	reference := fileReference(t, path)
	resolved, err := pathOf(reference)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != filepath.Clean(want) {
		t.Fatalf("resolved path = %q, want %q", resolved, want)
	}
}

func TestReferenceCanonicalizesPlatformPathsWithoutIO(t *testing.T) {
	paths := []string{
		filepath.Join(t.TempDir(), "space % 日本語.wav"),
		filepath.Join("relative", "nested", "input.raw"),
	}
	for _, path := range paths {
		reference, err := Reference(path)
		if err != nil {
			t.Fatalf("Reference(%q): %v", path, err)
		}
		got, err := pathOf(reference)
		if err != nil {
			t.Fatalf("pathOf(%q): %v", path, err)
		}
		want, err := filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.Clean(want) {
			t.Fatalf("Reference roundtrip = %q, want %q", got, filepath.Clean(want))
		}
	}
	if _, err := Reference(""); err == nil {
		t.Fatal("empty path accepted")
	}
	if _, err := Reference("bad\x00path"); err == nil {
		t.Fatal("NUL path accepted")
	}
}

func TestValidateDistinctRejectsEquivalentFileIdentities(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "input.wav")
	if err := os.WriteFile(input, []byte("input"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := map[string]string{
		"same path":              input,
		"relative normalization": filepath.Join(directory, "child", "..", "input.wav"),
	}
	link := filepath.Join(directory, "hardlink.wav")
	if err := os.Link(input, link); err != nil {
		t.Fatal(err)
	}
	tests["hard link"] = link
	symlink := filepath.Join(directory, "symlink.wav")
	if err := os.Symlink(input, symlink); err == nil {
		tests["symbolic link"] = symlink
	}
	if runtime.GOOS == "windows" {
		tests["path case"] = strings.ToUpper(input)
	}

	for name, output := range tests {
		t.Run(name, func(t *testing.T) {
			items := diagnostic.ItemsOf(ValidateDistinct(input, output))
			if len(items) != 1 || items[0].Code != "file.same-path" {
				t.Fatalf("same-file diagnostic = %#v", items)
			}
		})
	}
	if err := ValidateDistinct(input, filepath.Join(directory, "missing.wav")); err != nil {
		t.Fatalf("distinct missing output rejected: %v", err)
	}
	distinct := filepath.Join(directory, "existing.wav")
	if err := os.WriteFile(distinct, []byte("output"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDistinct(input, distinct); err != nil {
		t.Fatalf("distinct existing output rejected: %v", err)
	}
}

func TestReferenceCanonicalizesWindowsUNC(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows UNC syntax")
	}
	path := `\\server\share\space % 日本語.wav`
	reference, err := Reference(path)
	if err != nil {
		t.Fatal(err)
	}
	if reference.Canonical() != "file://server/share/space%20%25%20%E6%97%A5%E6%9C%AC%E8%AA%9E.wav" {
		t.Fatalf("UNC canonical reference = %q", reference.Canonical())
	}
	got, err := pathOf(reference)
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("UNC roundtrip = %q, want %q", got, path)
	}
}

func openedSink(t *testing.T, target string) (access.Session, *sinkOperator) {
	t.Helper()
	session, err := acquireSink(context.Background(), fileReference(t, target), selection(t, sinkCapabilities(), access.SequentialWrite))
	if err != nil {
		t.Fatal(err)
	}
	opening, err := access.NewOpening(access.SinkDirection, session, selection(t, sinkCapabilities(), access.SequentialWrite), access.AtomicReplace)
	if err != nil {
		session.Close()
		t.Fatal(err)
	}
	opened, err := openSink(sinkShape(), opening)
	if err != nil {
		session.Close()
		t.Fatal(err)
	}
	return session, opened.(*sinkOperator)
}

func writeBytes(t *testing.T, operator *sinkOperator, value []byte) {
	t.Helper()
	allocator, err := buffer.NewAllocator(int64(len(value)))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := allocator.FromBytes(value, 1)
	if err != nil {
		t.Fatal(err)
	}
	write, err := access.Append(handle)
	if err != nil {
		handle.Release()
		t.Fatal(err)
	}
	if err := operator.Write(context.Background(), flow.NewInput(write, access.Writes())); err != nil {
		handle.Release()
		t.Fatal(err)
	}
}

func selection(t *testing.T, available access.Capabilities, values ...access.Capability) access.Selection {
	t.Helper()
	selected, ok := access.Select(available, access.NewRequirements(access.AnyOf(values...)))
	if !ok {
		t.Fatalf("capability selection failed: available %v, requested %v", available.Values(), values)
	}
	return selected
}

func fileReference(t *testing.T, path string) access.Reference {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	value := filepath.ToSlash(absolute)
	if runtime.GOOS == "windows" && !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	reference, err := access.Parse((&url.URL{Scheme: "file", Path: value}).String())
	if err != nil {
		t.Fatal(err)
	}
	return reference
}

func temporaryFiles(t *testing.T, target string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".godec-*"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func assertFile(t *testing.T, path string, want []byte) {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(value, want) {
		t.Fatalf("file %q = %q, want %q", path, value, want)
	}
}
