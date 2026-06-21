//go:build !slim

package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"

	"github.com/gin-gonic/gin"
)

func registerWeChatPaymentOAuth(auth *gin.RouterGroup, h *handler.Handlers) {
	auth.GET("/oauth/wechat/payment/start", h.Auth.WeChatPaymentOAuthStart)
	auth.GET("/oauth/wechat/payment/callback", h.Auth.WeChatPaymentOAuthCallback)
}
