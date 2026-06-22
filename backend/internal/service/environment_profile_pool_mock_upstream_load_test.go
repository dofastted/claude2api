package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

const (
	mockUpstreamLoadEnv      = "SUB2API_MOCK_UPSTREAM_LOAD_TEST"
	mockUpstreamLoadDuration = 10 * time.Minute
	mockUpstreamLoadInterval = 50 * time.Millisecond
	mockUpstreamLoadAccounts = 20
	mockUpstreamLoadSlots    = 5
)

type mockUpstreamBehavior int

const (
	mockUpstreamBehaviorOK mockUpstreamBehavior = iota
	mockUpstreamBehaviorInvalidCredential
	mockUpstreamBehaviorForbidden
	mockUpstreamBehaviorQuotaExceeded
	mockUpstreamBehaviorOverloaded
	mockUpstreamBehaviorSSEError
	mockUpstreamBehaviorDisconnect
)

type mockUpstreamStats struct {
	total       atomic.Int64
	status200   atomic.Int64
	status401   atomic.Int64
	status403   atomic.Int64
	status429   atomic.Int64
	status503   atomic.Int64
	sseErrors   atomic.Int64
	disconnects atomic.Int64
	inflight    atomic.Int64
	maxInflight atomic.Int64
}

func (s *mockUpstreamStats) begin() {
	current := s.inflight.Add(1)
	for {
		max := s.maxInflight.Load()
		if current <= max || s.maxInflight.CompareAndSwap(max, current) {
			return
		}
	}
}

func (s *mockUpstreamStats) end() {
	s.inflight.Add(-1)
}

func (s *mockUpstreamStats) snapshot() mockUpstreamStatsSnapshot {
	return mockUpstreamStatsSnapshot{
		Total:       s.total.Load(),
		Status200:   s.status200.Load(),
		Status401:   s.status401.Load(),
		Status403:   s.status403.Load(),
		Status429:   s.status429.Load(),
		Status503:   s.status503.Load(),
		SSEErrors:   s.sseErrors.Load(),
		Disconnects: s.disconnects.Load(),
		Inflight:    s.inflight.Load(),
		MaxInflight: s.maxInflight.Load(),
	}
}

type mockUpstreamStatsSnapshot struct {
	Total       int64
	Status200   int64
	Status401   int64
	Status403   int64
	Status429   int64
	Status503   int64
	SSEErrors   int64
	Disconnects int64
	Inflight    int64
	MaxInflight int64
}

type mockUpstreamLoadHTTPUpstream struct {
	client *http.Client
}

func (u mockUpstreamLoadHTTPUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return nil, fmt.Errorf("unexpected Do call")
}

func (u mockUpstreamLoadHTTPUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.client.Do(req)
}

type mockUpstreamLoadResult struct {
	lease     *EnvironmentProfileSlotLease
	account   *Account
	response  *http.Response
	behavior  mockUpstreamBehavior
	failovers int
}

type mockUpstreamLoadSummary struct {
	Requests     int
	Successes    int
	Errors       int
	Failovers    int
	StatusCounts map[int]int
}

func TestClaudeProfilePoolMockUpstreamLoadSmoke(t *testing.T) {
	runClaudeProfilePoolMockUpstreamLoad(t, 10, 5*time.Second, mockUpstreamLoadInterval)
}

func TestClaudeProfilePoolMockUpstreamLoad100Concurrent10Minutes(t *testing.T) {
	if os.Getenv(mockUpstreamLoadEnv) != "1" {
		t.Skip("set SUB2API_MOCK_UPSTREAM_LOAD_TEST=1 to run the 100-concurrent 10-minute mock upstream load test")
	}
	runClaudeProfilePoolMockUpstreamLoad(t, 100, mockUpstreamLoadDuration, mockUpstreamLoadInterval)
}

func runClaudeProfilePoolMockUpstreamLoad(t *testing.T, concurrency int, duration time.Duration, interval time.Duration) {
	t.Helper()
	require.LessOrEqual(t, concurrency, mockUpstreamLoadAccounts*mockUpstreamLoadSlots)
	upstreamStats := &mockUpstreamStats{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamStats.begin()
		defer upstreamStats.end()
		upstreamStats.total.Add(1)
		behavior := mockUpstreamBehaviorFromHeader(r.Header.Get("X-Mock-Behavior"))
		time.Sleep(mockUpstreamLatency(r.Header.Get("X-Mock-Latency")))
		switch behavior {
		case mockUpstreamBehaviorInvalidCredential:
			upstreamStats.status401.Add(1)
			http.Error(w, `{"error":{"type":"authentication_error","message":"invalid credential"}}`, http.StatusUnauthorized)
		case mockUpstreamBehaviorForbidden:
			upstreamStats.status403.Add(1)
			http.Error(w, `{"error":{"type":"permission_error","message":"forbidden"}}`, http.StatusForbidden)
		case mockUpstreamBehaviorQuotaExceeded:
			upstreamStats.status429.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"type":"usage_limit_reached","message":"quota exceeded","resets_at":1777283883}}`))
		case mockUpstreamBehaviorOverloaded:
			upstreamStats.status503.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"type":"overloaded_error","message":"overloaded"}}`))
		case mockUpstreamBehaviorSSEError:
			upstreamStats.status200.Add(1)
			upstreamStats.sseErrors.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"stream failed\"}}\n\n"))
		case mockUpstreamBehaviorDisconnect:
			upstreamStats.disconnects.Add(1)
			if hijacker, ok := w.(http.Hijacker); ok {
				conn, _, err := hijacker.Hijack()
				if err == nil {
					_ = conn.Close()
					return
				}
			}
			upstreamStats.status503.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			upstreamStats.status200.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_mock\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-6\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n"))
			_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"))
			_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
		}
	}))
	defer server.Close()

	credentials := newClaudeProfilePoolTestCredentials(mockUpstreamLoadAccounts, mockUpstreamLoadSlots)
	for _, account := range credentials.accounts {
		account.Credentials = map[string]any{"access_token": "mock-token"}
		account.Extra["custom_base_url_enabled"] = true
		account.Extra["custom_base_url"] = server.URL
	}

	svc := &GatewayService{
		cfg:          &config.Config{},
		httpUpstream: mockUpstreamLoadHTTPUpstream{client: server.Client()},
	}
	svc.cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	summary := runMockUpstreamLoadRequests(t, svc, credentials, concurrency, duration, interval)
	stats := upstreamStats.snapshot()

	require.Equal(t, 0, credentials.activeCount())
	require.Equal(t, int64(0), stats.Inflight)
	require.GreaterOrEqual(t, int(stats.Total), summary.Requests)
	require.GreaterOrEqual(t, summary.Requests, concurrency)
	require.GreaterOrEqual(t, stats.MaxInflight, int64(concurrency/2))
	require.Greater(t, summary.Successes, 0)
	require.Greater(t, summary.Errors, 0)
	require.Greater(t, summary.Failovers, 0)
	require.Greater(t, stats.Status401, int64(0))
	require.Greater(t, stats.Status403, int64(0))
	require.Greater(t, stats.Status429, int64(0))
	require.Greater(t, stats.Status503, int64(0))
	require.Greater(t, stats.SSEErrors, int64(0))
	require.Greater(t, stats.Disconnects, int64(0))
}

func runMockUpstreamLoadRequests(t *testing.T, svc *GatewayService, credentials *claudeProfilePoolTestCredentials, concurrency int, duration time.Duration, interval time.Duration) mockUpstreamLoadSummary {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), duration+30*time.Second)
	defer cancel()
	plannedRequests := concurrency + int(duration/interval) + 2
	requests := make(chan int, concurrency*4)
	results := make(chan mockUpstreamLoadResult, plannedRequests)
	errs := make(chan error, plannedRequests)
	var wg sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range requests {
				result, err := dispatchClaudeMockUpstreamLoadRequest(ctx, svc, credentials, index)
				if err != nil {
					errs <- err
					continue
				}
				results <- result
			}
		}()
	}

	deadline := time.Now().Add(duration)
	sent := 0
	for sent < concurrency {
		requests <- sent
		sent++
	}
	if duration > 0 {
		ticker := time.NewTicker(interval)
		for now := range ticker.C {
			if !now.Before(deadline) {
				break
			}
			select {
			case requests <- sent:
				sent++
			case <-ctx.Done():
				ticker.Stop()
				close(requests)
				wg.Wait()
				close(results)
				close(errs)
				require.NoError(t, ctx.Err())
			}
		}
		ticker.Stop()
	}
	close(requests)
	wg.Wait()
	close(results)
	close(errs)

	summary := mockUpstreamLoadSummary{StatusCounts: map[int]int{}}
	for err := range errs {
		require.NoError(t, err)
	}
	for result := range results {
		summary.Requests++
		summary.Failovers += result.failovers
		if result.response == nil {
			summary.Errors++
			continue
		}
		summary.StatusCounts[result.response.StatusCode]++
		if result.response.StatusCode >= 200 && result.response.StatusCode < 300 && result.behavior != mockUpstreamBehaviorSSEError {
			summary.Successes++
			continue
		}
		summary.Errors++
	}
	require.Equal(t, sent, summary.Requests)
	return summary
}

func dispatchClaudeMockUpstreamLoadRequest(ctx context.Context, svc *GatewayService, credentials *claudeProfilePoolTestCredentials, index int) (mockUpstreamLoadResult, error) {
	environments := []EnvironmentClass{EnvironmentClassWindows, EnvironmentClassLinux, EnvironmentClassMacOS, EnvironmentClassDesktop}
	env := environments[index%len(environments)]
	behavior := mockUpstreamBehaviorForIndex(index)
	latencyClass := mockUpstreamLatencyClassForIndex(index)
	return dispatchClaudeMockUpstreamLoadAttempt(ctx, svc, credentials, env, behavior, latencyClass, index)
}

func dispatchClaudeMockUpstreamLoadAttempt(ctx context.Context, svc *GatewayService, credentials *claudeProfilePoolTestCredentials, env EnvironmentClass, behavior mockUpstreamBehavior, latencyClass string, startIndex int) (mockUpstreamLoadResult, error) {
	excluded := make(map[int64]struct{})
	failovers := 0
	for round := 0; ; round++ {
		select {
		case <-ctx.Done():
			return mockUpstreamLoadResult{}, ctx.Err()
		default:
		}
		blockedByCapacity := false
		for offset := 0; offset < len(credentials.accounts); offset++ {
			credentialIndex := (startIndex + round + offset) % len(credentials.accounts)
			account := credentials.accounts[credentialIndex]
			if _, ok := excluded[account.ID]; ok {
				continue
			}
			if !account.IsSchedulable() {
				excluded[account.ID] = struct{}{}
				failovers++
				continue
			}
			lease, profile, err := acquireClaudeEnvironmentProfileSlot(credentials.pools[credentialIndex], credentials.managers[credentialIndex], account, env, "", func(env EnvironmentClass) (*ClaudeEnvironmentProfile, error) {
				profile := buildClaudeEnvironmentProfileForClass(env)
				return profile, ValidateClaudeEnvironmentProfile(profile)
			})
			if err != nil {
				if errors.Is(err, ErrNoEnvironmentProfileSlot) {
					blockedByCapacity = true
					continue
				}
				return mockUpstreamLoadResult{}, err
			}
			if profile == nil || lease.Environment != env {
				lease.ReleaseFunc()
				return mockUpstreamLoadResult{}, fmt.Errorf("profile slot environment mismatch")
			}
			resp, err := sendMockUpstreamLoadRequest(ctx, svc, account, profile, behavior, latencyClass)
			lease.ReleaseFunc()
			if err != nil || shouldMockUpstreamBehaviorFailover(behavior, resp) {
				excluded[account.ID] = struct{}{}
				failovers++
				if !isRetryableMockUpstreamBehavior(behavior, resp) {
					return mockUpstreamLoadResult{lease: lease, account: account, response: resp, behavior: behavior, failovers: failovers}, nil
				}
				behavior = mockUpstreamBehaviorOK
				continue
			}
			return mockUpstreamLoadResult{lease: lease, account: account, response: resp, behavior: behavior, failovers: failovers}, nil
		}
		if len(excluded) == len(credentials.accounts) && !blockedByCapacity {
			return mockUpstreamLoadResult{}, ErrNoEnvironmentProfileSlot
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func sendMockUpstreamLoadRequest(ctx context.Context, svc *GatewayService, account *Account, profile *ClaudeEnvironmentProfile, behavior mockUpstreamBehavior, latencyClass string) (*http.Response, error) {
	baseURL := strings.TrimSpace(account.GetCustomBaseURL())
	if baseURL == "" {
		return nil, fmt.Errorf("missing mock upstream base url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/v1/messages?beta=true", strings.NewReader(mockUpstreamLoadRequestBody(indexStream(behavior))))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer mock-token")
	req.Header.Set("X-Mock-Behavior", mockUpstreamBehaviorName(behavior))
	req.Header.Set("X-Mock-Latency", latencyClass)
	svc.applyClaudeEnvironmentProfile(req, account, profile)
	resp, err := svc.httpUpstream.DoWithTLS(req, "", account.ID, account.Concurrency, nil)
	if err != nil {
		return nil, err
	}
	_, readErr := io.Copy(io.Discard, resp.Body)
	closeErr := resp.Body.Close()
	if readErr != nil {
		return resp, readErr
	}
	if closeErr != nil {
		return resp, closeErr
	}
	return resp, nil
}

func indexStream(behavior mockUpstreamBehavior) bool {
	return behavior == mockUpstreamBehaviorOK || behavior == mockUpstreamBehaviorSSEError || behavior == mockUpstreamBehaviorDisconnect
}

func mockUpstreamLoadRequestBody(stream bool) string {
	if stream {
		return `{"model":"claude-sonnet-4-6","max_tokens":16,"stream":true,"messages":[{"role":"user","content":[{"type":"text","text":"ping"}]}]}`
	}
	return `{"model":"claude-sonnet-4-6","max_tokens":16,"stream":false,"messages":[{"role":"user","content":[{"type":"text","text":"ping"}]}]}`
}

func mockUpstreamBehaviorForIndex(index int) mockUpstreamBehavior {
	switch {
	case index%100 == 0:
		return mockUpstreamBehaviorDisconnect
	case index%50 == 0:
		return mockUpstreamBehaviorSSEError
	case index%20 == 0:
		return mockUpstreamBehaviorInvalidCredential
	case index%20 == 1:
		return mockUpstreamBehaviorForbidden
	case index%20 == 2:
		return mockUpstreamBehaviorQuotaExceeded
	case index%20 == 3:
		return mockUpstreamBehaviorOverloaded
	default:
		return mockUpstreamBehaviorOK
	}
}

func mockUpstreamLatencyClassForIndex(index int) string {
	switch {
	case index%100 == 4:
		return "slow"
	case index%20 == 4:
		return "medium"
	default:
		return "fast"
	}
}

func mockUpstreamLatency(class string) time.Duration {
	switch class {
	case "slow":
		return 150 * time.Millisecond
	case "medium":
		return 25 * time.Millisecond
	default:
		return 2 * time.Millisecond
	}
}

func mockUpstreamBehaviorFromHeader(name string) mockUpstreamBehavior {
	switch name {
	case "invalid_credential":
		return mockUpstreamBehaviorInvalidCredential
	case "forbidden":
		return mockUpstreamBehaviorForbidden
	case "quota_exceeded":
		return mockUpstreamBehaviorQuotaExceeded
	case "overloaded":
		return mockUpstreamBehaviorOverloaded
	case "sse_error":
		return mockUpstreamBehaviorSSEError
	case "disconnect":
		return mockUpstreamBehaviorDisconnect
	default:
		return mockUpstreamBehaviorOK
	}
}

func mockUpstreamBehaviorName(behavior mockUpstreamBehavior) string {
	switch behavior {
	case mockUpstreamBehaviorInvalidCredential:
		return "invalid_credential"
	case mockUpstreamBehaviorForbidden:
		return "forbidden"
	case mockUpstreamBehaviorQuotaExceeded:
		return "quota_exceeded"
	case mockUpstreamBehaviorOverloaded:
		return "overloaded"
	case mockUpstreamBehaviorSSEError:
		return "sse_error"
	case mockUpstreamBehaviorDisconnect:
		return "disconnect"
	default:
		return "ok"
	}
}

func shouldMockUpstreamBehaviorFailover(behavior mockUpstreamBehavior, resp *http.Response) bool {
	if behavior == mockUpstreamBehaviorDisconnect {
		return true
	}
	if resp == nil {
		return false
	}
	return resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
}

func isRetryableMockUpstreamBehavior(behavior mockUpstreamBehavior, resp *http.Response) bool {
	if behavior == mockUpstreamBehaviorDisconnect {
		return true
	}
	if resp == nil {
		return false
	}
	return resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
}
