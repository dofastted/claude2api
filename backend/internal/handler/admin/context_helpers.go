package admin

import (
	middleware2 "github.com/dofastted/claude2api/internal/server/middleware"

	"github.com/gin-gonic/gin"
)

func getAdminIDFromContext(c *gin.Context) int64 {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		return 0
	}
	return subject.UserID
}
