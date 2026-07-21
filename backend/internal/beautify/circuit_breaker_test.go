package beautify

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCircuitBreakerHalfOpenAllowsOneProbe(t *testing.T) {
	cb := NewCircuitBreakerWithConfig(CircuitBreakerConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		Timeout:          time.Millisecond,
	})
	cb.RecordFailure()
	time.Sleep(2 * time.Millisecond)

	var allowed atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if !cb.IsOpen() {
				allowed.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := allowed.Load(); got != 1 {
		t.Fatalf("half-open allowed %d probes, want 1", got)
	}
	cb.RecordSuccess()
	if cb.IsOpen() {
		t.Fatal("circuit should close after the successful probe")
	}
}

func TestCircuitBreakerHalfOpenFailureReopens(t *testing.T) {
	cb := NewCircuitBreakerWithConfig(CircuitBreakerConfig{
		FailureThreshold: 1,
		SuccessThreshold: 2,
		Timeout:          time.Millisecond,
	})
	cb.RecordFailure()
	time.Sleep(2 * time.Millisecond)
	if cb.IsOpen() {
		t.Fatal("expected one half-open probe")
	}
	cb.RecordFailure()
	if !cb.IsOpen() {
		t.Fatal("failed half-open probe must reopen the circuit")
	}
}
