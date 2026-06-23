package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var genericProbeUserAgentPrefixes = []string{
	"go-http-client/",
	"curl/",
	"wget/",
	"python-requests/",
}

func IsGenericProbeUserAgent(ua string) bool {
	ua = strings.ToLower(strings.TrimSpace(ua))
	if ua == "" {
		return true
	}
	for _, prefix := range genericProbeUserAgentPrefixes {
		if strings.HasPrefix(ua, prefix) {
			return true
		}
	}
	return false
}

type EnvironmentClass string

const (
	EnvironmentClassWindows EnvironmentClass = "windows"
	EnvironmentClassLinux   EnvironmentClass = "linux"
	EnvironmentClassMacOS   EnvironmentClass = "macos"
	EnvironmentClassDesktop EnvironmentClass = "desktop"
)

type EnvironmentProfileSlotState string

const (
	EnvironmentProfileSlotEmpty EnvironmentProfileSlotState = "empty"
	EnvironmentProfileSlotBound EnvironmentProfileSlotState = "bound"
)

type EnvironmentProfileSlotLease struct {
	AccountID   int64
	Slot        int
	Environment EnvironmentClass
	RequestID   string
	BoundNew    bool
	ReleaseFunc func()
}

type environmentProfileLeaseContextKey struct{}

type releaseOnCloseReadCloser struct {
	io.ReadCloser
	release func()
	once    sync.Once
}

func (r *releaseOnCloseReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.once.Do(r.release)
	return err
}

func attachEnvironmentProfileLeaseToRequest(req *http.Request, lease *EnvironmentProfileSlotLease) *http.Request {
	if req == nil || lease == nil || lease.ReleaseFunc == nil {
		return req
	}
	return req.WithContext(contextWithEnvironmentProfileLease(req.Context(), lease.ReleaseFunc))
}

func contextWithEnvironmentProfileLease(ctx context.Context, release func()) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, environmentProfileLeaseContextKey{}, release)
}

func releaseEnvironmentProfileLeaseFromRequest(req *http.Request) {
	if req == nil {
		return
	}
	release, _ := req.Context().Value(environmentProfileLeaseContextKey{}).(func())
	if release != nil {
		release()
	}
}

func wrapResponseBodyWithEnvironmentProfileLease(req *http.Request, resp *http.Response) {
	if req == nil || resp == nil || resp.Body == nil {
		return
	}
	release, _ := req.Context().Value(environmentProfileLeaseContextKey{}).(func())
	if release == nil {
		return
	}
	resp.Body = &releaseOnCloseReadCloser{ReadCloser: resp.Body, release: release}
}

type EnvironmentProfileSlotLeaseManager struct {
	mu        sync.Mutex
	active    map[string]string
	poolMu    sync.Mutex
	poolLocks map[int64]*sync.Mutex
}

func NewEnvironmentProfileSlotLeaseManager() *EnvironmentProfileSlotLeaseManager {
	return &EnvironmentProfileSlotLeaseManager{active: make(map[string]string), poolLocks: make(map[int64]*sync.Mutex)}
}

func (m *EnvironmentProfileSlotLeaseManager) lockAccount(accountID int64) func() {
	if m == nil {
		return func() {}
	}
	m.poolMu.Lock()
	if m.poolLocks == nil {
		m.poolLocks = make(map[int64]*sync.Mutex)
	}
	lock := m.poolLocks[accountID]
	if lock == nil {
		lock = &sync.Mutex{}
		m.poolLocks[accountID] = lock
	}
	m.poolMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func (m *EnvironmentProfileSlotLeaseManager) activeCount() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.active)
}

func (m *EnvironmentProfileSlotLeaseManager) release(accountID int64, slot int, requestID string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := environmentProfileSlotLeaseKey(accountID, slot)
	if current := m.active[key]; current == requestID {
		delete(m.active, key)
	}
}

func (m *EnvironmentProfileSlotLeaseManager) acquire(accountID int64, capacity int, requestID string, choose func(isActive func(int) bool) (int, error)) (*EnvironmentProfileSlotLease, error) {
	if m == nil {
		m = NewEnvironmentProfileSlotLeaseManager()
	}
	if capacity <= 0 {
		capacity = 1
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		requestID = generateRequestID()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	isActive := func(slot int) bool {
		_, ok := m.active[environmentProfileSlotLeaseKey(accountID, slot)]
		return ok
	}
	slot, err := choose(isActive)
	if err != nil {
		return nil, err
	}
	if slot < 0 || slot >= capacity {
		return nil, ErrNoEnvironmentProfileSlot
	}
	key := environmentProfileSlotLeaseKey(accountID, slot)
	if _, exists := m.active[key]; exists {
		return nil, ErrNoEnvironmentProfileSlot
	}
	m.active[key] = requestID
	lease := &EnvironmentProfileSlotLease{AccountID: accountID, Slot: slot, RequestID: requestID}
	lease.ReleaseFunc = func() { m.release(accountID, slot, requestID) }
	return lease, nil
}

var ErrNoEnvironmentProfileSlot = fmt.Errorf("no environment profile slot available")

func environmentProfileSlotExhaustedError() error {
	return &UpstreamFailoverError{
		StatusCode:   http.StatusServiceUnavailable,
		ResponseBody: []byte(`{"error":{"type":"overloaded_error","message":"environment profile slots exhausted"},"type":"error"}`),
	}
}

func environmentProfileSlotLeaseKey(accountID int64, slot int) string {
	return strconv.FormatInt(accountID, 10) + ":" + strconv.Itoa(slot)
}

func normalizeEnvironmentClass(env EnvironmentClass) EnvironmentClass {
	switch EnvironmentClass(strings.ToLower(strings.TrimSpace(string(env)))) {
	case EnvironmentClassWindows:
		return EnvironmentClassWindows
	case EnvironmentClassLinux:
		return EnvironmentClassLinux
	case EnvironmentClassMacOS:
		return EnvironmentClassMacOS
	case EnvironmentClassDesktop:
		return EnvironmentClassDesktop
	default:
		return EnvironmentClassWindows
	}
}

// AccountEnvironmentProfileCapacity returns the fixed profile slot capacity.
// safe for concurrent calls: it only reads immutable account fields.
func AccountEnvironmentProfileCapacity(account *Account) int {
	return environmentProfileCapacity(account)
}

func environmentProfileCapacity(account *Account) int {
	if account == nil {
		return 1
	}
	if capacity := parseEnvironmentProfileCapacity(account.Extra[environmentProfileManualCapacityKey]); capacity > 0 {
		return capacity
	}
	if capacity := accountTierEnvironmentProfileCapacity(account); capacity > 0 {
		return capacity
	}
	if account.Concurrency > 0 {
		return account.Concurrency
	}
	return 1
}

const environmentProfileManualCapacityKey = "environment_profile_manual_capacity"

func accountTierEnvironmentProfileCapacity(account *Account) int {
	if account == nil {
		return 0
	}
	switch account.Platform {
	case PlatformAnthropic:
		return claudeTierEnvironmentProfileCapacity(account.ClaudeAccountTier())
	case PlatformOpenAI:
		return codexTierEnvironmentProfileCapacity(account.CodexAccountTier())
	default:
		return 0
	}
}

func claudeTierEnvironmentProfileCapacity(tier string) int {
	switch normalizeClaudeAccountTier(tier) {
	case "pro":
		return 5
	case "max5":
		return 10
	case "max20":
		return 20
	default:
		return 0
	}
}

func codexTierEnvironmentProfileCapacity(tier string) int {
	switch normalizeCodexAccountTier(tier) {
	case "plus":
		return 5
	case "pro5":
		return 10
	case "pro20", "team":
		return 20
	default:
		return 0
	}
}

func firstEnvironmentProfileTierValue(account *Account, keys []string) string {
	if account == nil {
		return ""
	}
	for _, key := range keys {
		if value := strings.TrimSpace(account.GetCredential(key)); value != "" {
			return value
		}
	}
	for _, key := range keys {
		if value := strings.TrimSpace(account.GetExtraString(key)); value != "" {
			return value
		}
	}
	return ""
}

func normalizeClaudeAccountTier(raw string) string {
	value := normalizeEnvironmentProfileTierToken(raw)
	switch {
	case strings.Contains(value, "max20") || strings.Contains(value, "maxx20") || strings.Contains(value, "20x"):
		return "max20"
	case strings.Contains(value, "max5") || strings.Contains(value, "maxx5") || strings.Contains(value, "5x"):
		return "max5"
	case strings.Contains(value, "pro"):
		return "pro"
	default:
		return ""
	}
}

func normalizeCodexAccountTier(raw string) string {
	value := normalizeEnvironmentProfileTierToken(raw)
	switch {
	case strings.Contains(value, "team"):
		return "team"
	case strings.Contains(value, "pro20") || strings.Contains(value, "20x"):
		return "pro20"
	case strings.Contains(value, "pro5") || strings.Contains(value, "5x"):
		return "pro5"
	case strings.Contains(value, "plus"):
		return "plus"
	default:
		return ""
	}
}

func normalizeEnvironmentProfileTierToken(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	replacer := strings.NewReplacer(" ", "", "-", "", "_", "", ".", "")
	return replacer.Replace(value)
}

func parseEnvironmentProfileCapacity(raw any) int {
	switch v := raw.(type) {
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	case json.Number:
		if n, err := v.Int64(); err == nil && n > 0 {
			return int(n)
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func DetectClaudeEnvironmentClass(headers http.Header, body []byte) EnvironmentClass {
	if classifyClaudeClientFamily(headers, body) == ClaudeClientFamilyDesktop {
		return EnvironmentClassDesktop
	}
	return detectEnvironmentClassFromHeaders(headers)
}

func DetectCodexEnvironmentClass(headers http.Header) EnvironmentClass {
	if detectCodexClientFamilyFromHeaders(headers) == CodexClientFamilyDesktop {
		return EnvironmentClassDesktop
	}
	return detectEnvironmentClassFromHeaders(headers)
}

func detectEnvironmentClassFromHeaders(headers http.Header) EnvironmentClass {
	if headers == nil {
		return EnvironmentClassWindows
	}
	values := []string{
		headerValueCaseInsensitive(headers, "X-Stainless-OS"),
		headerValueCaseInsensitive(headers, "sec-ch-ua-platform"),
		headerValueCaseInsensitive(headers, "User-Agent"),
		headerValueCaseInsensitive(headers, "originator"),
		headerValueCaseInsensitive(headers, "platform"),
	}
	combined := strings.ToLower(strings.Join(values, "\n"))
	if strings.Contains(combined, "windows") || strings.Contains(combined, "win32") {
		return EnvironmentClassWindows
	}
	if strings.Contains(combined, "darwin") || strings.Contains(combined, "mac os") || strings.Contains(combined, "macos") || strings.Contains(combined, "macintosh") {
		return EnvironmentClassMacOS
	}
	if strings.Contains(combined, "linux") || strings.Contains(combined, "ubuntu") || strings.Contains(combined, "x11") {
		return EnvironmentClassLinux
	}
	return EnvironmentClassWindows
}

func headerValueCaseInsensitive(headers http.Header, key string) string {
	if headers == nil {
		return ""
	}
	if value := strings.TrimSpace(headers.Get(key)); value != "" {
		return value
	}
	for headerKey, values := range headers {
		if !strings.EqualFold(headerKey, key) {
			continue
		}
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func nowForEnvironmentProfilePool() time.Time {
	return time.Now().UTC().Truncate(time.Second)
}
