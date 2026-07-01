package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dofastted/claude2api/internal/config"
	"github.com/dofastted/claude2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type claudeTelemetryProbeCapture struct {
	Header http.Header
	Body   []byte
}

type claudeTelemetryProbeUpstream struct {
	client *http.Client
}

func (u claudeTelemetryProbeUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return nil, fmt.Errorf("unexpected Do call")
}

func (u claudeTelemetryProbeUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.client.Do(req)
}

func TestClaudeTelemetryProbeDataReachesMockUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	captures := make(chan claudeTelemetryProbeCapture, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		captures <- claudeTelemetryProbeCapture{Header: r.Header.Clone(), Body: body}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_mock","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude-sonnet-4-6","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	account := &Account{
		ID:          9201,
		Name:        "claude-telemetry-probe",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 3,
		Credentials: map[string]any{"access_token": "oauth-token", "email": "owner@example.com", "plan_type": "max"},
		Extra: map[string]any{
			"account_uuid":            "acc-telemetry",
			"org_uuid":                "org-telemetry",
			"custom_base_url_enabled": true,
			"custom_base_url":         server.URL,
		},
	}
	repo := &environmentProfileAccountRepo{account: account}
	svc := &GatewayService{
		cfg:                                &config.Config{},
		accountRepo:                        repo,
		identityService:                    NewIdentityService(&identityCacheStub{}, nil),
		httpUpstream:                       claudeTelemetryProbeUpstream{client: server.Client()},
		claudeEnvironmentProfileSlotLeases: NewEnvironmentProfileSlotLeaseManager(),
		rateLimitService:                   &RateLimitService{},
	}
	svc.cfg.Security.URLAllowlist.AllowInsecureHTTP = true

	clientUserID := FormatMetadataUserID(
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"client-account",
		"11111111-2222-4333-8444-555555555555",
		"2.1.191",
	)
	body := []byte(`{"model":"claude-sonnet-4-6","max_tokens":16,"stream":false,"metadata":{"user_id":` + strconvQuote(clientUserID) + `},"messages":[{"role":"user","content":[{"type":"text","text":"ping"}]}]}`)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.191 (external, cli)")
	c.Request.Header.Set("X-Stainless-OS", "Linux")
	c.Request.Header.Set("X-App", "claude-code")
	c.Request.Header.Set("Anthropic-Client-Type", "cli")
	c.Request.Header.Set("X-Claude-Code-Session-Id", "client-session-must-not-leak")

	req, wireBody, err := svc.buildUpstreamRequest(context.Background(), c, account, body, "oauth-token", "oauth", "claude-sonnet-4-6", false, false)
	require.NoError(t, err)
	resp, err := svc.httpUpstream.DoWithTLS(req, "", account.ID, account.Concurrency, nil)
	require.NoError(t, err)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	var capture claudeTelemetryProbeCapture
	select {
	case capture = <-captures:
	default:
		t.Fatal("mock upstream did not receive request")
	}

	pool, err := DecodeClaudeEnvironmentProfilePool(repo.account.Extra[claudeEnvironmentProfilePoolKey])
	require.NoError(t, err)
	require.NotNil(t, pool)
	slot := pool.Slots[2]
	require.Equal(t, EnvironmentClassLinux, slot.Environment)
	profile := slot.Profile
	require.NotNil(t, profile)

	metadataUserID := gjson.GetBytes(capture.Body, "metadata.user_id").String()
	parsed := ParseMetadataUserID(metadataUserID)
	require.NotNil(t, parsed)
	require.Equal(t, stableClaudeTelemetryUserID(account.ID, "linux"), parsed.DeviceID)
	require.Equal(t, stableClaudeTelemetrySessionID(account.ID, "linux"), parsed.SessionID)
	require.Equal(t, "acc-telemetry", parsed.AccountUUID)
	require.NotContains(t, string(capture.Body), clientUserID)
	require.JSONEq(t, string(wireBody), string(capture.Body))

	require.Equal(t, profile.TelemetrySessionID, capture.Header.Get("X-Claude-Code-Session-Id"))
	require.NotEqual(t, "client-session-must-not-leak", capture.Header.Get("X-Claude-Code-Session-Id"))
	require.Equal(t, profile.UserAgent, capture.Header.Get("User-Agent"))
	require.Equal(t, profile.Platform, capture.Header.Get("X-Stainless-OS"))
	require.Equal(t, profile.ClientVersion, capture.Header.Get("X-Stainless-Package-Version"))
	require.Equal(t, profile.RuntimeVersion, capture.Header.Get("X-Stainless-Runtime-Version"))

	require.Equal(t, parsed.DeviceID, profile.TelemetryUserID)
	require.Equal(t, parsed.SessionID, profile.TelemetrySessionID)
	require.Equal(t, parsed.DeviceID, profile.StatsigStableID)
	require.Equal(t, parsed.DeviceID, profile.TelemetryAttributes["user.id"])
	require.Equal(t, parsed.SessionID, profile.TelemetryAttributes["session.id"])
	require.Equal(t, "owner@example.com", profile.TelemetryAttributes["user.email"])
	require.Equal(t, "org-telemetry", profile.TelemetryAttributes["organization.id"])
	require.Equal(t, parsed.DeviceID, profile.FeatureFlagAttributes["deviceID"])
	require.Equal(t, parsed.SessionID, profile.FeatureFlagAttributes["sessionId"])
	require.Equal(t, "acc-telemetry", profile.FeatureFlagAttributes["accountUUID"])
	require.Equal(t, "org-telemetry", profile.FeatureFlagAttributes["organizationUUID"])
	require.Empty(t, profile.FeatureFlagAttributes["subscriptionType"])
}
