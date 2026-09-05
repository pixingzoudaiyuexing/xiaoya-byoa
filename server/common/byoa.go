package common

import (
	"errors"

	"github.com/OpenListTeam/OpenList/v4/internal/byoa"
	"github.com/gin-gonic/gin"
)

// BYOAAuthData 是前端识别“需要网盘扫码授权”的最小结构。
// HTTP 仍沿用 OpenList 现有统一响应格式，前端只需判断 data.byoa_auth_required。
type BYOAAuthData struct {
	BYOAAuthRequired bool          `json:"byoa_auth_required"`
	Provider         byoa.Provider `json:"provider"`
}

// TryBYOAAuthResp 若 err 是 BYOA 授权缺失错误，则直接写入结构化响应并返回 true。
// 非 BYOA 错误返回 false，由调用方继续走原有错误处理。
func TryBYOAAuthResp(c *gin.Context, err error) bool {
	var authErr *byoa.AuthRequiredError
	if !errors.As(err, &authErr) {
		return false
	}
	ErrorWithDataResp(c, err, 401, BYOAAuthData{
		BYOAAuthRequired: true,
		Provider:         authErr.Provider,
	})
	return true
}
