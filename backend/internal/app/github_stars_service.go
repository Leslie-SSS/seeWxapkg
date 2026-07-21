package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	githubRepositoryEndpoint  = "https://api.github.com/repos/Leslie-SSS/seeWxapkg"
	githubAPIVersion          = "2022-11-28"
	githubStarsFreshFor       = 5 * time.Minute
	githubStarsStaleFor       = 24 * time.Hour
	githubStarsRetryAfter     = time.Minute
	githubStarsRequestTimeout = 4 * time.Second
	githubStarsMaxResponse    = int64(64 * 1024)
)

// ErrGitHubStarsUnavailable means that GitHub could not be safely queried and
// no previously successful value is available. It intentionally carries no
// upstream response details so callers cannot accidentally expose them.
var ErrGitHubStarsUnavailable = errors.New("github star count is temporarily unavailable")

// GitHubStars is the public, minimal result needed by the website.
type GitHubStars struct {
	Count int64
	Stale bool
}

type githubStarsCache struct {
	count     int64
	fetchedAt time.Time
}

type githubStarsFlight struct {
	done   chan struct{}
	result GitHubStars
	err    error
}

// GitHubStarsService fetches one fixed public GitHub repository. The endpoint
// is not configurable through requests or environment variables, which keeps
// this feature from becoming an outbound proxy or SSRF primitive.
type GitHubStarsService struct {
	client           *http.Client
	endpoint         string
	now              func() time.Time
	freshFor         time.Duration
	staleFor         time.Duration
	retryAfter       time.Duration
	maxResponseBytes int64

	mu        sync.Mutex
	cache     *githubStarsCache
	inFlight  *githubStarsFlight
	nextRetry time.Time
}

// NewGitHubStarsService constructs the production service with a fixed GitHub
// API URL and a tightly bounded HTTP client.
func NewGitHubStarsService() *GitHubStarsService {
	return newGitHubStarsService(githubStarsServiceConfig{})
}

type githubStarsServiceConfig struct {
	client           *http.Client
	endpoint         string
	now              func() time.Time
	freshFor         time.Duration
	staleFor         time.Duration
	retryAfter       time.Duration
	requestTimeout   time.Duration
	maxResponseBytes int64
}

func newGitHubStarsService(cfg githubStarsServiceConfig) *GitHubStarsService {
	requestTimeout := cfg.requestTimeout
	if requestTimeout <= 0 {
		requestTimeout = githubStarsRequestTimeout
	}

	transport := http.RoundTripper(newGitHubTransport())
	if cfg.client != nil && cfg.client.Transport != nil {
		transport = cfg.client.Transport
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   requestTimeout,
		// A redirect must never be able to move this fixed request away from the
		// reviewed GitHub API endpoint.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	endpoint := cfg.endpoint
	if endpoint == "" {
		endpoint = githubRepositoryEndpoint
	}
	now := cfg.now
	if now == nil {
		now = time.Now
	}
	freshFor := cfg.freshFor
	if freshFor <= 0 {
		freshFor = githubStarsFreshFor
	}
	staleFor := cfg.staleFor
	if staleFor <= 0 {
		staleFor = githubStarsStaleFor
	}
	retryAfter := cfg.retryAfter
	if retryAfter <= 0 {
		retryAfter = githubStarsRetryAfter
	}
	maxResponseBytes := cfg.maxResponseBytes
	if maxResponseBytes <= 0 {
		maxResponseBytes = githubStarsMaxResponse
	}

	return &GitHubStarsService{
		client:           client,
		endpoint:         endpoint,
		now:              now,
		freshFor:         freshFor,
		staleFor:         staleFor,
		retryAfter:       retryAfter,
		maxResponseBytes: maxResponseBytes,
	}
}

func newGitHubTransport() *http.Transport {
	return &http.Transport{
		// Do not inherit HTTP(S)_PROXY. This fixed, public metadata request
		// should connect only to GitHub rather than an ambient third party.
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           4,
		MaxIdleConnsPerHost:    2,
		MaxConnsPerHost:        4,
		IdleConnTimeout:        90 * time.Second,
		TLSHandshakeTimeout:    2 * time.Second,
		ResponseHeaderTimeout:  3 * time.Second,
		ExpectContinueTimeout:  time.Second,
		MaxResponseHeaderBytes: 16 * 1024,
	}
}

// Get returns a five-minute fresh cache whenever possible. Only one refresh is
// allowed at a time; concurrent callers wait for that same bounded request. A
// canceled browser request stops waiting but does not cancel the shared fetch.
func (s *GitHubStarsService) Get(ctx context.Context) (GitHubStars, error) {
	s.mu.Lock()
	now := s.now()
	if s.cache != nil && !now.Before(s.cache.fetchedAt.Add(s.staleFor)) {
		s.cache = nil
	}
	if s.cache != nil && now.Before(s.cache.fetchedAt.Add(s.freshFor)) {
		result := GitHubStars{Count: s.cache.count}
		s.mu.Unlock()
		return result, nil
	}
	if now.Before(s.nextRetry) {
		if s.cache != nil {
			result := GitHubStars{Count: s.cache.count, Stale: true}
			s.mu.Unlock()
			return result, nil
		}
		s.mu.Unlock()
		return GitHubStars{}, ErrGitHubStarsUnavailable
	}

	flight := s.inFlight
	if flight == nil {
		flight = &githubStarsFlight{done: make(chan struct{})}
		s.inFlight = flight
		go s.refresh(flight)
	}
	s.mu.Unlock()

	select {
	case <-ctx.Done():
		return GitHubStars{}, ctx.Err()
	case <-flight.done:
		return flight.result, flight.err
	}
}

func (s *GitHubStarsService) refresh(flight *githubStarsFlight) {
	count, err := s.fetch()
	now := s.now()

	s.mu.Lock()
	if err == nil {
		s.cache = &githubStarsCache{count: count, fetchedAt: now}
		s.nextRetry = time.Time{}
		flight.result = GitHubStars{Count: count}
	} else {
		s.nextRetry = now.Add(s.retryAfter)
		if s.cache != nil && !now.Before(s.cache.fetchedAt.Add(s.staleFor)) {
			s.cache = nil
		}
		if s.cache != nil {
			flight.result = GitHubStars{Count: s.cache.count, Stale: true}
		} else {
			flight.err = ErrGitHubStarsUnavailable
		}
	}
	s.inFlight = nil
	close(flight.done)
	s.mu.Unlock()
}

func (s *GitHubStarsService) fetch() (int64, error) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, s.endpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("build GitHub request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	request.Header.Set("User-Agent", "seeWxapkg-star-counter")

	response, err := s.client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("request GitHub repository: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected GitHub status: %d", response.StatusCode)
	}
	if err := validateGitHubJSONContentType(response.Header.Get("Content-Type")); err != nil {
		return 0, err
	}
	if response.ContentLength > s.maxResponseBytes {
		return 0, fmt.Errorf("GitHub response is too large")
	}

	limited := io.LimitReader(response.Body, s.maxResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return 0, fmt.Errorf("read GitHub response: %w", err)
	}
	if int64(len(body)) > s.maxResponseBytes {
		return 0, fmt.Errorf("GitHub response is too large")
	}

	var payload struct {
		StargazersCount *int64 `json:"stargazers_count"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, fmt.Errorf("decode GitHub response: %w", err)
	}
	if payload.StargazersCount == nil || *payload.StargazersCount < 0 {
		return 0, fmt.Errorf("GitHub response has an invalid star count")
	}
	return *payload.StargazersCount, nil
}

func validateGitHubJSONContentType(value string) error {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return fmt.Errorf("GitHub response has an invalid content type")
	}
	if mediaType != "application/json" && mediaType != "application/vnd.github+json" {
		return fmt.Errorf("GitHub response has an unexpected content type")
	}
	return nil
}
