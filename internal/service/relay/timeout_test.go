package relay

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type blockingReadCloser struct {
	closed chan struct{}
	once   sync.Once
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{closed: make(chan struct{})}
}

func (r *blockingReadCloser) Read([]byte) (int, error) {
	<-r.closed
	return 0, errors.New("closed")
}

func (r *blockingReadCloser) Close() error {
	r.once.Do(func() { close(r.closed) })
	return nil
}

func TestStreamTimeoutReaderStopsWaitingForFirstByte(t *testing.T) {
	reader := newStreamTimeoutReader(newBlockingReadCloser(), 15*time.Millisecond, 0)
	startedAt := time.Now()
	_, err := reader.Read(make([]byte, 1))
	if time.Since(startedAt) > time.Second {
		t.Fatal("first-byte timeout did not interrupt the blocked read")
	}
	var timeout upstreamTimeoutError
	if !errors.As(err, &timeout) || timeout.phase != "stream first-byte" {
		t.Fatalf("unexpected stream timeout error: %v", err)
	}
}
