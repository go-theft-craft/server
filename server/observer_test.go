package server_test

import (
	"sync"
	"testing"
	"time"

	"github.com/go-theft-craft/server/server"
)

// collectingObserver stores every sample it is given.
type collectingObserver struct {
	mu      sync.Mutex
	samples []server.Sample
}

func (o *collectingObserver) Observe(s server.Sample) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.samples = append(o.samples, s)
}

func (o *collectingObserver) count(kind server.SampleKind) int {
	o.mu.Lock()
	defer o.mu.Unlock()

	n := 0
	for _, s := range o.samples {
		if s.Kind == kind {
			n++
		}
	}

	return n
}

// await waits for the observer to have seen at least want samples of a kind.
// Delivery is asynchronous, so a test that read the count immediately would be
// asserting on the scheduler rather than on the seam.
func (o *collectingObserver) await(t *testing.T, kind server.SampleKind, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if o.count(kind) >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}

	t.Fatalf("observer saw %d %s samples, want %d", o.count(kind), kind, want)
}

// blockingObserver never returns, which is how a badly written observer
// stalls a server that calls it on a hot path.
type blockingObserver struct{ release chan struct{} }

func (o *blockingObserver) Observe(server.Sample) { <-o.release }

func TestAServerWithNoObserverUsesTheNopAndDoesNotPanic(t *testing.T) {
	srv, err := server.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// A nil observer here would panic on the first sample, which would
	// only show up under load.
	srv.Observe(server.Sample{Kind: server.SampleCPU, Value: 1})
}

func TestWithObserverReceivesSamples(t *testing.T) {
	obs := &collectingObserver{}

	srv, err := server.New(server.WithObserver(obs))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	srv.Observe(server.Sample{Kind: server.SampleNetworkIn, Value: 42})

	obs.await(t, server.SampleNetworkIn, 1)
}

func TestWithObserverRejectsNil(t *testing.T) {
	if _, err := server.New(server.WithObserver(nil)); err == nil {
		t.Error("WithObserver accepted nil; omit the option to run without one")
	}
}

func TestASlowObserverDoesNotStallTheCaller(t *testing.T) {
	// Delivery to an observer must never block the caller, because the
	// network sink runs on the stream's own goroutine and blocking there
	// applies backpressure to the whole connection.
	obs := &blockingObserver{release: make(chan struct{})}

	srv, err := server.New(server.WithObserver(obs))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			srv.Observe(server.Sample{Kind: server.SampleCPU, Value: float64(i)})
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Observe blocked on a slow observer")
	}
	close(obs.release)
}
