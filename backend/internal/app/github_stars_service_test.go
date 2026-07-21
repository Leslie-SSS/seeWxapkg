package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGitHubStarsServiceUsesFixedSecureProductionDefaults(t *testing.T) {
	service := NewGitHubStarsService()
	if service.endpoint != "https://api.github.com/repos/Leslie-SSS/seeWxapkg" {
		t.Fatalf("unexpected production endpoint: %q", service.endpoint)
	}
	if service.client.Timeout != 4*time.Second {
		t.Fatalf("HTTP timeout = %s, want 4s", service.client.Timeout)
	}
	if service.freshFor != 5*time.Minute || service.staleFor != 24*time.Hour {
		t.Fatalf("cache windows = %s/%s, want 5m/24h", service.freshFor, service.staleFor)
	}
	transport, ok := service.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected production transport type: %T", service.client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("production GitHub client must not inherit an environment proxy")
	}
}

func TestGitHubStarsServiceFetchesValidatedCountAndCachesIt(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/repos/Leslie-SSS/seeWxapkg" || r.URL.RawQuery != "" {
			t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL.RequestURI())
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("X-GitHub-Api-Version") != githubAPIVersion {
			t.Errorf("required GitHub headers missing: %#v", r.Header)
		}
		for _, header := range []string{"Authorization", "Cookie", "Forwarded", "X-Forwarded-For", "X-Real-IP"} {
			if value := r.Header.Get(header); value != "" {
				t.Errorf("visitor or credential header %s was forwarded: %q", header, value)
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{"stargazers_count":321,"ignored":"safe"}`)
	}))
	defer server.Close()

	service := newGitHubStarsService(githubStarsServiceConfig{
		client:   server.Client(),
		endpoint: server.URL + "/repos/Leslie-SSS/seeWxapkg",
	})
	first, err := service.Get(context.Background())
	if err != nil || first.Count != 321 || first.Stale {
		t.Fatalf("first result = %#v, err=%v", first, err)
	}
	second, err := service.Get(context.Background())
	if err != nil || second != first {
		t.Fatalf("cached result = %#v, err=%v", second, err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want 1", got)
	}
}

func TestGitHubStarsServiceRefreshesAfterFiveMinutes(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"stargazers_count":%d}`, 40+count)
	}))
	defer server.Close()

	service := newGitHubStarsService(githubStarsServiceConfig{
		client:   server.Client(),
		endpoint: server.URL,
		now:      func() time.Time { return now },
	})
	first, err := service.Get(context.Background())
	if err != nil || first.Count != 41 {
		t.Fatalf("first result = %#v, err=%v", first, err)
	}
	now = now.Add(5*time.Minute - time.Nanosecond)
	stillFresh, err := service.Get(context.Background())
	if err != nil || stillFresh.Count != 41 || requests.Load() != 1 {
		t.Fatalf("fresh cache was not reused: result=%#v err=%v requests=%d", stillFresh, err, requests.Load())
	}
	now = now.Add(time.Nanosecond)
	refreshed, err := service.Get(context.Background())
	if err != nil || refreshed.Count != 42 || requests.Load() != 2 {
		t.Fatalf("expired cache was not refreshed: result=%#v err=%v requests=%d", refreshed, err, requests.Load())
	}
}

func TestGitHubStarsServiceDeduplicatesConcurrentRefreshes(t *testing.T) {
	var requests atomic.Int32
	requestStarted := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			close(requestStarted)
		}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"stargazers_count":88}`)
	}))
	defer server.Close()
	service := newGitHubStarsService(githubStarsServiceConfig{client: server.Client(), endpoint: server.URL})

	const callers = 32
	start := make(chan struct{})
	errorsSeen := make(chan error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			<-start
			result, err := service.Get(context.Background())
			if err != nil {
				errorsSeen <- err
				return
			}
			if result.Count != 88 || result.Stale {
				errorsSeen <- fmt.Errorf("unexpected result: %#v", result)
			}
		}()
	}
	close(start)
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("shared refresh did not start")
	}
	close(release)
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Error(err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("concurrent upstream requests = %d, want 1", got)
	}
}

func TestGitHubStarsServiceUsesBoundedStaleValueOnFailure(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	startedAt := now
	var requests atomic.Int32
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"stargazers_count":73}`)
	}))
	defer server.Close()
	service := newGitHubStarsService(githubStarsServiceConfig{
		client:   server.Client(),
		endpoint: server.URL,
		now:      func() time.Time { return now },
	})

	if result, err := service.Get(context.Background()); err != nil || result.Count != 73 {
		t.Fatalf("initial fetch = %#v, err=%v", result, err)
	}
	fail.Store(true)
	now = startedAt.Add(5 * time.Minute)
	stale, err := service.Get(context.Background())
	if err != nil || stale.Count != 73 || !stale.Stale {
		t.Fatalf("stale fallback = %#v, err=%v", stale, err)
	}
	if _, err := service.Get(context.Background()); err != nil || requests.Load() != 2 {
		t.Fatalf("failure cooldown was not honored: err=%v requests=%d", err, requests.Load())
	}

	// At the 24-hour boundary the value is too old to present as current.
	now = startedAt.Add(24 * time.Hour)
	result, err := service.Get(context.Background())
	if !errors.Is(err, ErrGitHubStarsUnavailable) || result != (GitHubStars{}) {
		t.Fatalf("expired stale value was exposed: result=%#v err=%v", result, err)
	}
	if requests.Load() != 3 {
		t.Fatalf("upstream requests = %d, want 3", requests.Load())
	}
}

func TestGitHubStarsServiceRejectsInvalidUpstreamResponses(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		maxBody int64
	}{
		{"status", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }, 0},
		{"content type", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, `{"stargazers_count":1}`)
		}, 0},
		{"malformed JSON", jsonResponse(`{"stargazers_count":`), 0},
		{"missing count", jsonResponse(`{"name":"seeWxapkg"}`), 0},
		{"negative count", jsonResponse(`{"stargazers_count":-1}`), 0},
		{"fractional count", jsonResponse(`{"stargazers_count":1.5}`), 0},
		{"string count", jsonResponse(`{"stargazers_count":"100"}`), 0},
		{"oversized declared body", jsonResponse(`{"padding":"` + strings.Repeat("x", 256) + `","stargazers_count":1}`), 64},
		{"oversized chunked body", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			_, _ = io.WriteString(w, `{"padding":"`+strings.Repeat("x", 256)+`","stargazers_count":1}`)
		}, 64},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(test.handler)
			defer server.Close()
			service := newGitHubStarsService(githubStarsServiceConfig{
				client:           server.Client(),
				endpoint:         server.URL,
				maxResponseBytes: test.maxBody,
			})
			result, err := service.Get(context.Background())
			if !errors.Is(err, ErrGitHubStarsUnavailable) || result != (GitHubStars{}) {
				t.Fatalf("invalid response accepted: result=%#v err=%v", result, err)
			}
		})
	}
}

func TestGitHubStarsServiceRejectsRedirectsAndEnforcesTimeout(t *testing.T) {
	t.Run("redirect", func(t *testing.T) {
		var redirected atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/redirected" {
				redirected.Add(1)
				jsonResponse(`{"stargazers_count":999}`)(w, r)
				return
			}
			http.Redirect(w, r, "/redirected", http.StatusFound)
		}))
		defer server.Close()
		service := newGitHubStarsService(githubStarsServiceConfig{client: server.Client(), endpoint: server.URL})
		if _, err := service.Get(context.Background()); !errors.Is(err, ErrGitHubStarsUnavailable) {
			t.Fatalf("redirect was accepted: %v", err)
		}
		if redirected.Load() != 0 {
			t.Fatal("GitHub redirect target was followed")
		}
	})

	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		defer server.Close()
		service := newGitHubStarsService(githubStarsServiceConfig{
			client:         server.Client(),
			endpoint:       server.URL,
			requestTimeout: 25 * time.Millisecond,
		})
		started := time.Now()
		if _, err := service.Get(context.Background()); !errors.Is(err, ErrGitHubStarsUnavailable) {
			t.Fatalf("timed-out request returned: %v", err)
		}
		if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
			t.Fatalf("timeout was not bounded: %s", elapsed)
		}
	})
}

func jsonResponse(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}
}
