package beautify

import (
	"sync"
	"time"
)

// State represents the circuit breaker state
type State int

const (
	// StateClosed means requests are allowed through
	StateClosed State = iota
	// StateOpen means requests are blocked
	StateOpen
	// StateHalfOpen means we're testing if the service is healthy again
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	mu sync.RWMutex

	// Configuration
	failureThreshold  int           // Number of failures before opening
	successThreshold  int           // Number of successes in half-open to close
	timeout           time.Duration // Time to wait before trying half-open

	// State
	state          State
	failures       int
	successes      int
	lastFailure    time.Time
	lastStateChange time.Time
}

// CircuitBreakerConfig holds configuration for the circuit breaker
type CircuitBreakerConfig struct {
	FailureThreshold int
	SuccessThreshold int
	Timeout          time.Duration
}

// NewCircuitBreaker creates a new circuit breaker with default settings
func NewCircuitBreaker() *CircuitBreaker {
	return NewCircuitBreakerWithConfig(CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
	})
}

// NewCircuitBreakerWithConfig creates a new circuit breaker with custom settings
func NewCircuitBreakerWithConfig(cfg CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: cfg.FailureThreshold,
		successThreshold: cfg.SuccessThreshold,
		timeout:          cfg.Timeout,
		lastStateChange:  time.Now(),
	}
}

// IsOpen returns true if the circuit breaker is open (requests should be blocked)
func (cb *CircuitBreaker) IsOpen() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.state == StateOpen {
		// Check if timeout has passed to try half-open
		if time.Since(cb.lastFailure) > cb.timeout {
			cb.mu.RUnlock()
			cb.mu.Lock()
			// Double check after acquiring write lock
			if cb.state == StateOpen && time.Since(cb.lastFailure) > cb.timeout {
				cb.state = StateHalfOpen
				cb.successes = 0
				cb.lastStateChange = time.Now()
			}
			cb.mu.Unlock()
			cb.mu.RLock()
			return false // Allow one request through in half-open
		}
		return true
	}

	return false
}

// RecordSuccess records a successful operation
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0

	switch cb.state {
	case StateHalfOpen:
		cb.successes++
		if cb.successes >= cb.successThreshold {
			cb.state = StateClosed
			cb.successes = 0
			cb.lastStateChange = time.Now()
		}
	case StateClosed:
		// Already closed, nothing to do
	}
}

// RecordFailure records a failed operation
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailure = time.Now()

	switch cb.state {
	case StateClosed:
		cb.failures++
		if cb.failures >= cb.failureThreshold {
			cb.state = StateOpen
			cb.lastStateChange = time.Now()
		}
	case StateHalfOpen:
		// Any failure in half-open goes back to open
		cb.state = StateOpen
		cb.lastStateChange = time.Now()
	}
}

// GetState returns the current state
func (cb *CircuitBreaker) GetState() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetStats returns current statistics
func (cb *CircuitBreaker) GetStats() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return map[string]interface{}{
		"state":            cb.state.String(),
		"failures":         cb.failures,
		"successes":        cb.successes,
		"failureThreshold": cb.failureThreshold,
		"successThreshold": cb.successThreshold,
		"lastFailure":      cb.lastFailure,
		"lastStateChange":  cb.lastStateChange,
	}
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = StateClosed
	cb.failures = 0
	cb.successes = 0
	cb.lastStateChange = time.Now()
}
