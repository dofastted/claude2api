package service

import (
	"net/http"
	"testing"
	"time"
)

func TestClassifyClaudeRateLimit_ExtraUsage(t *testing.T) {
	s := &RateLimitService{}
	headers := http.Header{}
	body := []byte(`{"error": {"message": "Your account requires extra usage required to continue"}}`)

	rateLimitType, cooldown := s.classifyClaudeRateLimit(429, headers, body)

	if rateLimitType != ClaudeRateLimitTypeExtraUsage {
		t.Errorf("Expected ClaudeRateLimitTypeExtraUsage, got %s", rateLimitType)
	}
	if cooldown != 24*time.Hour {
		t.Errorf("Expected 24h cooldown, got %v", cooldown)
	}
}

func TestClassifyClaudeRateLimit_ExtraUsage_CaseInsensitive(t *testing.T) {
	s := &RateLimitService{}
	headers := http.Header{}
	body := []byte(`{"error": {"message": "EXTRA USAGE REQUIRED to proceed"}}`)

	rateLimitType, cooldown := s.classifyClaudeRateLimit(429, headers, body)

	if rateLimitType != ClaudeRateLimitTypeExtraUsage {
		t.Errorf("Expected ClaudeRateLimitTypeExtraUsage, got %s", rateLimitType)
	}
	if cooldown != 24*time.Hour {
		t.Errorf("Expected 24h cooldown, got %v", cooldown)
	}
}

func TestClassifyClaudeRateLimit_OpusWeekly(t *testing.T) {
	s := &RateLimitService{}
	headers := http.Header{}
	body := []byte(`{"error": {"message": "You have exceeded the claude-opus-4 models per week limit"}}`)

	rateLimitType, cooldown := s.classifyClaudeRateLimit(429, headers, body)

	if rateLimitType != ClaudeRateLimitTypeOpusWeekly {
		t.Errorf("Expected ClaudeRateLimitTypeOpusWeekly, got %s", rateLimitType)
	}
	if cooldown != 168*time.Hour {
		t.Errorf("Expected 168h cooldown, got %v", cooldown)
	}
}

func TestClassifyClaudeRateLimit_OpusWeekly_CaseInsensitive(t *testing.T) {
	s := &RateLimitService{}
	headers := http.Header{}
	body := []byte(`{"error": {"message": "CLAUDE-OPUS-3.5 MODELS PER WEEK limit exceeded"}}`)

	rateLimitType, cooldown := s.classifyClaudeRateLimit(429, headers, body)

	if rateLimitType != ClaudeRateLimitTypeOpusWeekly {
		t.Errorf("Expected ClaudeRateLimitTypeOpusWeekly, got %s", rateLimitType)
	}
	if cooldown != 168*time.Hour {
		t.Errorf("Expected 168h cooldown, got %v", cooldown)
	}
}

func TestClassifyClaudeRateLimit_5HourWindow(t *testing.T) {
	s := &RateLimitService{}
	headers := http.Header{}
	body := []byte(`{"error": {"message": "Rate limit exceeded within 5 hour window"}}`)

	rateLimitType, cooldown := s.classifyClaudeRateLimit(429, headers, body)

	if rateLimitType != ClaudeRateLimitType5HourWindow {
		t.Errorf("Expected ClaudeRateLimitType5HourWindow, got %s", rateLimitType)
	}
	if cooldown != 5*time.Hour {
		t.Errorf("Expected 5h cooldown, got %v", cooldown)
	}
}

func TestClassifyClaudeRateLimit_Generic(t *testing.T) {
	s := &RateLimitService{}
	headers := http.Header{}
	body := []byte(`{"error": {"message": "Rate limit exceeded"}}`)

	rateLimitType, cooldown := s.classifyClaudeRateLimit(429, headers, body)

	if rateLimitType != ClaudeRateLimitTypeGeneric {
		t.Errorf("Expected ClaudeRateLimitTypeGeneric, got %s", rateLimitType)
	}
	if cooldown != 1*time.Hour {
		t.Errorf("Expected 1h cooldown, got %v", cooldown)
	}
}

func TestClassifyClaudeRateLimit_Unknown_WithRetryAfter(t *testing.T) {
	s := &RateLimitService{}
	headers := http.Header{}
	headers.Set("retry-after", "3600")
	body := []byte(`{"error": {"message": "Some other rate limit"}}`)

	rateLimitType, cooldown := s.classifyClaudeRateLimit(429, headers, body)

	if rateLimitType != ClaudeRateLimitTypeUnknown {
		t.Errorf("Expected ClaudeRateLimitTypeUnknown, got %s", rateLimitType)
	}
	if cooldown != 0 {
		t.Errorf("Expected 0 cooldown (fallback to retry-after), got %v", cooldown)
	}
}

func TestClassifyClaudeRateLimit_Unknown_WithRateLimitReset(t *testing.T) {
	s := &RateLimitService{}
	headers := http.Header{}
	headers.Set("x-ratelimit-reset", "1719849600")
	body := []byte(`{"error": {"message": "Rate limit exceeded"}}`)

	rateLimitType, cooldown := s.classifyClaudeRateLimit(429, headers, body)

	if rateLimitType != ClaudeRateLimitTypeUnknown {
		t.Errorf("Expected ClaudeRateLimitTypeUnknown, got %s", rateLimitType)
	}
	if cooldown != 0 {
		t.Errorf("Expected 0 cooldown (fallback to header parsing), got %v", cooldown)
	}
}

func TestClassifyClaudeRateLimit_Priority_ExtraUsageOverOpusWeekly(t *testing.T) {
	s := &RateLimitService{}
	headers := http.Header{}
	// Body 包含两个关键字，Extra Usage 优先级更高
	body := []byte(`{"error": {"message": "extra usage required and claude-opus models per week limit"}}`)

	rateLimitType, cooldown := s.classifyClaudeRateLimit(429, headers, body)

	if rateLimitType != ClaudeRateLimitTypeExtraUsage {
		t.Errorf("Expected ClaudeRateLimitTypeExtraUsage (highest priority), got %s", rateLimitType)
	}
	if cooldown != 24*time.Hour {
		t.Errorf("Expected 24h cooldown, got %v", cooldown)
	}
}

func TestClassifyClaudeRateLimit_Priority_OpusWeeklyOver5HourWindow(t *testing.T) {
	s := &RateLimitService{}
	headers := http.Header{}
	// Body 包含两个关键字，Opus Weekly 优先级更高
	body := []byte(`{"error": {"message": "claude-opus models per week limit in 5 hour window"}}`)

	rateLimitType, cooldown := s.classifyClaudeRateLimit(429, headers, body)

	if rateLimitType != ClaudeRateLimitTypeOpusWeekly {
		t.Errorf("Expected ClaudeRateLimitTypeOpusWeekly (higher priority), got %s", rateLimitType)
	}
	if cooldown != 168*time.Hour {
		t.Errorf("Expected 168h cooldown, got %v", cooldown)
	}
}

func TestClassifyClaudeRateLimit_NonClaude_StatusCode(t *testing.T) {
	s := &RateLimitService{}
	headers := http.Header{}
	body := []byte(`{"error": {"message": "extra usage required"}}`)

	// 非 429 状态码应返回 Unknown
	rateLimitType, cooldown := s.classifyClaudeRateLimit(500, headers, body)

	if rateLimitType != ClaudeRateLimitTypeUnknown {
		t.Errorf("Expected ClaudeRateLimitTypeUnknown for non-429 status, got %s", rateLimitType)
	}
	if cooldown != 0 {
		t.Errorf("Expected 0 cooldown for non-429 status, got %v", cooldown)
	}
}

func TestClassifyClaudeRateLimit_EmptyBody(t *testing.T) {
	s := &RateLimitService{}
	headers := http.Header{}
	body := []byte(``)

	rateLimitType, cooldown := s.classifyClaudeRateLimit(429, headers, body)

	// 空 body 且无 retry-after header → Generic
	if rateLimitType != ClaudeRateLimitTypeGeneric {
		t.Errorf("Expected ClaudeRateLimitTypeGeneric for empty body, got %s", rateLimitType)
	}
	if cooldown != 1*time.Hour {
		t.Errorf("Expected 1h cooldown, got %v", cooldown)
	}
}
