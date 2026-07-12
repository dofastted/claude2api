package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/dofastted/claude2api/internal/pkg/response"
	"github.com/dofastted/claude2api/internal/service"
	"github.com/gin-gonic/gin"
)

type ClaudeOAuthPoolHandler struct {
	service *service.ClaudeOAuthPoolAdminService
}

func NewClaudeOAuthPoolHandler(poolService *service.ClaudeOAuthPoolAdminService) *ClaudeOAuthPoolHandler {
	return &ClaudeOAuthPoolHandler{service: poolService}
}

type claudeOAuthPoolRequest struct {
	Name           string   `json:"name" binding:"required"`
	Status         string   `json:"status"`
	EgressRouteID  int64    `json:"egress_route_id" binding:"required"`
	AllowedOrigins []string `json:"allowed_origins" binding:"required"`
	AllowedModels  []string `json:"allowed_models" binding:"required"`
}

type claudeOAuthCredentialRequest struct {
	AccountID int64 `json:"account_id" binding:"required"`
}

type claudeOAuthCapsuleRequest struct {
	Version         int64  `json:"version" binding:"required"`
	CLIVersion      string `json:"cli_version" binding:"required"`
	ProfileTimezone string `json:"profile_timezone"`
}

type claudeOAuthModeRequest struct {
	Mode string `json:"mode" binding:"required"`
}

func (h *ClaudeOAuthPoolHandler) List(c *gin.Context) {
	pools, err := h.service.ListPools(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, pools)
}

func (h *ClaudeOAuthPoolHandler) Get(c *gin.Context) {
	poolID, ok := claudeOAuthPoolID(c)
	if !ok {
		return
	}
	pool, err := h.service.GetPool(c.Request.Context(), poolID)
	if err != nil {
		claudeOAuthPoolError(c, err)
		return
	}
	credentials, err := h.service.ListCredentials(c.Request.Context(), poolID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	bindingCounts, err := h.service.BindingCounts(c.Request.Context(), poolID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	metrics, err := h.service.ShadowMetrics(c.Request.Context(), poolID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"pool": pool, "credentials": credentials, "binding_counts": bindingCounts, "shadow_metrics": metrics})
}

func (h *ClaudeOAuthPoolHandler) Create(c *gin.Context) {
	var req claudeOAuthPoolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	pool, err := h.service.CreatePool(c.Request.Context(), &service.OAuthPool{
		Name: req.Name, Status: req.Status, EgressRouteID: req.EgressRouteID,
		AllowedOrigins: req.AllowedOrigins, AllowedModels: req.AllowedModels,
	})
	if err != nil {
		claudeOAuthPoolError(c, err)
		return
	}
	response.Success(c, pool)
}

func (h *ClaudeOAuthPoolHandler) Update(c *gin.Context) {
	poolID, ok := claudeOAuthPoolID(c)
	if !ok {
		return
	}
	var req claudeOAuthPoolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	pool, err := h.service.UpdatePool(c.Request.Context(), poolID, &service.OAuthPool{
		Name: req.Name, Status: req.Status, EgressRouteID: req.EgressRouteID,
		AllowedOrigins: req.AllowedOrigins, AllowedModels: req.AllowedModels,
	})
	if err != nil {
		claudeOAuthPoolError(c, err)
		return
	}
	response.Success(c, pool)
}

func (h *ClaudeOAuthPoolHandler) Delete(c *gin.Context) {
	poolID, ok := claudeOAuthPoolID(c)
	if !ok {
		return
	}
	if err := h.service.DeletePool(c.Request.Context(), poolID); err != nil {
		claudeOAuthPoolError(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *ClaudeOAuthPoolHandler) AddCredential(c *gin.Context) {
	poolID, ok := claudeOAuthPoolID(c)
	if !ok {
		return
	}
	var req claudeOAuthCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	credential, err := h.service.AddCredential(c.Request.Context(), poolID, req.AccountID)
	if err != nil {
		claudeOAuthPoolError(c, err)
		return
	}
	response.Success(c, credential)
}

func (h *ClaudeOAuthPoolHandler) RemoveCredential(c *gin.Context) {
	poolID, ok := claudeOAuthPoolID(c)
	if !ok {
		return
	}
	accountID, err := strconv.ParseInt(c.Param("account_id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if err := h.service.RemoveCredential(c.Request.Context(), poolID, accountID); err != nil {
		claudeOAuthPoolError(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *ClaudeOAuthPoolHandler) CreateCapsule(c *gin.Context) {
	poolID, ok := claudeOAuthPoolID(c)
	if !ok {
		return
	}
	var req claudeOAuthCapsuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	capsule, err := h.service.CreateCapsuleSet(c.Request.Context(), poolID, req.Version, req.CLIVersion, req.ProfileTimezone)
	if err != nil {
		claudeOAuthPoolError(c, err)
		return
	}
	response.Success(c, capsule)
}

func (h *ClaudeOAuthPoolHandler) ActivateCapsule(c *gin.Context) {
	poolID, ok := claudeOAuthPoolID(c)
	if !ok {
		return
	}
	version, err := strconv.ParseInt(c.Param("version"), 10, 64)
	if err != nil || version <= 0 {
		response.BadRequest(c, "Invalid capsule version")
		return
	}
	pool, err := h.service.ActivateCapsuleSet(c.Request.Context(), poolID, version)
	if err != nil {
		claudeOAuthPoolError(c, err)
		return
	}
	response.Success(c, pool)
}

func (h *ClaudeOAuthPoolHandler) ShadowMetrics(c *gin.Context) {
	poolID, ok := claudeOAuthPoolID(c)
	if !ok {
		return
	}
	metrics, err := h.service.ShadowMetrics(c.Request.Context(), poolID)
	if err != nil {
		claudeOAuthPoolError(c, err)
		return
	}
	response.Success(c, metrics)
}

func (h *ClaudeOAuthPoolHandler) ResetCredentialBindings(c *gin.Context) {
	poolID, ok := claudeOAuthPoolID(c)
	if !ok {
		return
	}
	accountID, err := strconv.ParseInt(c.Param("account_id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	deleted, err := h.service.ResetCredentialBindings(c.Request.Context(), poolID, accountID)
	if err != nil {
		claudeOAuthPoolError(c, err)
		return
	}
	response.Success(c, gin.H{"deleted_bindings": deleted})
}

func (h *ClaudeOAuthPoolHandler) SetMode(c *gin.Context) {
	poolID, ok := claudeOAuthPoolID(c)
	if !ok {
		return
	}
	var req claudeOAuthModeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	pool, err := h.service.SetMode(c.Request.Context(), poolID, req.Mode)
	if err != nil {
		claudeOAuthPoolError(c, err)
		return
	}
	response.Success(c, pool)
}

func claudeOAuthPoolID(c *gin.Context) (int64, bool) {
	poolID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || poolID <= 0 {
		response.BadRequest(c, "Invalid OAuth pool ID")
		return 0, false
	}
	return poolID, true
}

func claudeOAuthPoolError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrOAuthPoolInvalid), errors.Is(err, service.ErrOAuthPoolCredentialInvalid):
		response.BadRequest(c, err.Error())
	case errors.Is(err, service.ErrOAuthPoolCredentialConflict), errors.Is(err, service.ErrOAuthCapsuleSetConflict), errors.Is(err, service.ErrOAuthPoolEnforceGateNotReached):
		response.Error(c, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrOAuthPoolNotFound), errors.Is(err, service.ErrOAuthCapsuleSetNotFound):
		response.NotFound(c, err.Error())
	default:
		response.ErrorFrom(c, err)
	}
}
