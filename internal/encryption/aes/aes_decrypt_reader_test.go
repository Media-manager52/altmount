package aes

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// blockingSource is a source whose Read blocks until Close, like a
// UsenetReader waiting on a slow NNTP article.
type blockingSource struct {
	closeOnce sync.Once
	closed    chan struct{}
}

func newBlockingSource() *blockingSource {
	return &blockingSource{closed: make(chan struct{})}
}

func (s *blockingSource) Read(p []byte) (int, error) {
	<-s.closed
	return 0, io.ErrClosedPipe
}

func (s *blockingSource) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}

func (s *blockingSource) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

func newTestReader(t *testing.T, getReader func(ctx context.Context, start, end int64) (io.ReadCloser, error)) *aesDecryptReader {
	t.Helper()
	r, err := newAesDecryptReader(context.Background(), getReader, make([]byte, 16), make([]byte, 16), 4096, 4096, -1)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func waitRead(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Read did not return after Close")
		return nil
	}
}

// Close runs on another goroutine while Read is blocked on the source; this is
// what readFullContext does when a FUSE read times out. It used to race on
// the source field and could call Close on a torn interface (nil receiver).
func TestCloseInterruptsBlockedRead(t *testing.T) {
	source := newBlockingSource()
	entered := make(chan struct{})
	r := newTestReader(t, func(context.Context, int64, int64) (io.ReadCloser, error) {
		close(entered)
		return source, nil
	})

	done := make(chan error, 1)
	go func() {
		_, err := r.Read(make([]byte, 16))
		done <- err
	}()
	<-entered
	// Give Read a moment to block on the source, then close concurrently.
	time.Sleep(20 * time.Millisecond)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := waitRead(t, done); err == nil {
		t.Fatal("Read after Close returned no error")
	}
	if !source.isClosed() {
		t.Fatal("source was not closed")
	}
}

// Close runs while Read is still creating the source. The late source must be
// closed (not leaked) and Read must report the reader as closed.
func TestCloseDuringLazySourceCreation(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	source := newBlockingSource()
	r := newTestReader(t, func(context.Context, int64, int64) (io.ReadCloser, error) {
		close(entered)
		<-release
		return source, nil
	})

	done := make(chan error, 1)
	go func() {
		_, err := r.Read(make([]byte, 16))
		done <- err
	}()
	<-entered
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := waitRead(t, done); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("Read error = %v, want io.ErrClosedPipe", err)
	}
	if !source.isClosed() {
		t.Fatal("source created after Close was leaked")
	}
	if err := r.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
