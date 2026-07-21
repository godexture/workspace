package cli

import (
	"io"
	"sync/atomic"
)

type inputMetrics struct {
	BytesRead uint64
	ReadCalls uint64
	SeekCalls uint64
	Position  int64
	Size      int64
}

type measuredReadSeeker struct {
	reader    io.ReadSeeker
	size      int64
	bytesRead atomic.Uint64
	readCalls atomic.Uint64
	seekCalls atomic.Uint64
	position  atomic.Int64
}

func newMeasuredReadSeeker(reader io.ReadSeeker, size int64) *measuredReadSeeker {
	return &measuredReadSeeker{reader: reader, size: size}
}

func (reader *measuredReadSeeker) Read(data []byte) (int, error) {
	n, err := reader.reader.Read(data)
	reader.readCalls.Add(1)
	reader.bytesRead.Add(uint64(n))
	reader.position.Add(int64(n))
	return n, err
}

func (reader *measuredReadSeeker) Seek(offset int64, whence int) (int64, error) {
	position, err := reader.reader.Seek(offset, whence)
	reader.seekCalls.Add(1)
	if err == nil {
		reader.position.Store(position)
	}
	return position, err
}

func (reader *measuredReadSeeker) Snapshot() inputMetrics {
	return inputMetrics{
		BytesRead: reader.bytesRead.Load(),
		ReadCalls: reader.readCalls.Load(),
		SeekCalls: reader.seekCalls.Load(),
		Position:  reader.position.Load(),
		Size:      reader.size,
	}
}

type outputMetrics struct {
	BytesWritten uint64
	WriteCalls   uint64
}

type measuredWriter struct {
	writer       io.Writer
	bytesWritten atomic.Uint64
	writeCalls   atomic.Uint64
}

func newMeasuredWriter(writer io.Writer) *measuredWriter {
	return &measuredWriter{writer: writer}
}

func (writer *measuredWriter) Write(data []byte) (int, error) {
	n, err := writer.writer.Write(data)
	writer.writeCalls.Add(1)
	writer.bytesWritten.Add(uint64(n))
	return n, err
}

func (writer *measuredWriter) Snapshot() outputMetrics {
	return outputMetrics{
		BytesWritten: writer.bytesWritten.Load(),
		WriteCalls:   writer.writeCalls.Load(),
	}
}
