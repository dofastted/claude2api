package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/dofastted/claude2api/internal/server/middleware"
	"github.com/dofastted/claude2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIssueClaudeOAuthSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resolver, err := service.NewClaudeOAuthSessionResolver(service.ClaudeOAuthSessionKeys{
		CurrentSigningKeyID: "current",
		CurrentSigningKey:   []byte("0123456789abcdef0123456789abcdef"),
		BindingKey:          []byte("abcdef0123456789abcdef0123456789"),
	})
	require.NoError(t, err)

	poolID := int64(17)
	h := &GatewayHandler{}
	h.SetClaudeOAuthSessionResolver(resolver)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID: 23,
			Group: &service.Group{
				ID:          29,
				OAuthPoolID: &poolID,
			},
		})
	})
	router.POST("/v1/session", h.IssueClaudeOAuthSession)

	req := httptest.NewRequest(http.MethodPost, "/v1/session", bytes.NewBufferString(`{"native_session_id":"native-session"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusCreated, recorder.Code)
	token := recorder.Header().Get(service.ClaudeOAuthSignedSessionHeader)
	require.NotEmpty(t, token)
	resolved, err := resolver.Resolve(service.ClaudeOAuthSessionNamespace{GroupID: 29, APIKeyID: 23}, service.ClaudeOAuthSessionInput{
		SignedToken:      token,
		NativeCandidates: []string{"native-session"},
	})
	require.NoError(t, err)
	require.Equal(t, "signed", resolved.Source)
	require.NotEmpty(t, resolved.NativeHash)
}

func TestIssueClaudeOAuthSessionRejectsConflictingNativeIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resolver, err := service.NewClaudeOAuthSessionResolver(service.ClaudeOAuthSessionKeys{
		CurrentSigningKeyID: "current",
		CurrentSigningKey:   []byte("0123456789abcdef0123456789abcdef"),
		BindingKey:          []byte("abcdef0123456789abcdef0123456789"),
	})
	require.NoError(t, err)

	poolID := int64(17)
	h := &GatewayHandler{}
	h.SetClaudeOAuthSessionResolver(resolver)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID:    23,
			Group: &service.Group{ID: 29, OAuthPoolID: &poolID},
		})
	})
	router.POST("/v1/session", h.IssueClaudeOAuthSession)

	req := httptest.NewRequest(http.MethodPost, "/v1/session", bytes.NewBufferString(`{"native_session_id":"body-session"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(service.ClaudeOAuthNativeSessionHeader, "header-session")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), "conflicting_session_id")
}

func TestResolveClaudeOAuthSessionUsesOnlyAuthenticatedSessionInputs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resolver, err := service.NewClaudeOAuthSessionResolver(service.ClaudeOAuthSessionKeys{
		CurrentSigningKeyID: "current",
		CurrentSigningKey:   []byte("0123456789abcdef0123456789abcdef"),
		BindingKey:          []byte("abcdef0123456789abcdef0123456789"),
	})
	require.NoError(t, err)
	h := &GatewayHandler{}
	h.SetClaudeOAuthSessionResolver(resolver)
	poolID := int64(17)
	apiKey := &service.APIKey{ID: 23, Group: &service.Group{ID: 29, OAuthPoolID: &poolID}}

	request := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	resolved, err := h.resolveClaudeOAuthSession(context, apiKey, "metadata-session")
	require.NoError(t, err)
	require.Equal(t, "native", resolved.Source)
	require.NotEmpty(t, resolved.BindingHash)

	issued, err := resolver.Issue(service.ClaudeOAuthSessionNamespace{GroupID: 29, APIKeyID: 23}, "signed-native")
	require.NoError(t, err)
	request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	request.Header.Set(service.ClaudeOAuthSignedSessionHeader, issued.SignedToken)
	context, _ = gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	_, err = h.resolveClaudeOAuthSession(context, apiKey, "different-native")
	require.ErrorIs(t, err, service.ErrClaudeOAuthSessionConflict)

	context, _ = gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	_, err = h.resolveClaudeOAuthSession(context, apiKey, "")
	require.ErrorIs(t, err, service.ErrClaudeOAuthSessionMissing)
}
