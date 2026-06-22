package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSimpleModeDoesNotRegisterAnnouncementRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	settingService := service.NewSettingService(nil, &config.Config{RunMode: config.RunModeSimple})
	noAuth := gin.HandlerFunc(func(c *gin.Context) { c.Next() })
	handlers := &handler.Handlers{
		Announcement: handler.NewAnnouncementHandler(nil),
		Admin: &handler.AdminHandlers{
			Announcement: adminhandler.NewAnnouncementHandler(nil),
		},
	}

	RegisterUserRoutes(v1, handlers, middleware.JWTAuthMiddleware(noAuth), settingService)
	RegisterAdminRoutes(v1, handlers, middleware.AdminAuthMiddleware(noAuth), settingService)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/v1/announcements"},
		{method: http.MethodPost, path: "/api/v1/announcements/1/read"},
		{method: http.MethodGet, path: "/api/v1/admin/announcements"},
		{method: http.MethodPost, path: "/api/v1/admin/announcements"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, nil)
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusNotFound, rec.Code, "%s %s", tc.method, tc.path)
	}
}
