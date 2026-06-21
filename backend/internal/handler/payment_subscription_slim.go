//go:build slim

package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type PaymentHandler struct{}

type PaymentWebhookHandler struct{}

type SubscriptionHandler struct{}

func (h *SubscriptionHandler) List(c *gin.Context) {
	subscriptionDisabled(c)
}

func (h *SubscriptionHandler) GetActive(c *gin.Context) {
	subscriptionDisabled(c)
}

func (h *SubscriptionHandler) GetProgress(c *gin.Context) {
	subscriptionDisabled(c)
}

func (h *SubscriptionHandler) GetSummary(c *gin.Context) {
	subscriptionDisabled(c)
}

func subscriptionDisabled(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{
		"success": false,
		"reason":  "subscription is disabled in slim build",
	})
}
