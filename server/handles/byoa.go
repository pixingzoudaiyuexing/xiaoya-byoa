package handles

import (
	"github.com/OpenListTeam/OpenList/v4/internal/byoa"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
)

// BYOAQuarkStart 创建夸克扫码二维码。
// token 由浏览器持有，服务端不创建 Session。
func BYOAQuarkStart(c *gin.Context) {
	result, err := byoa.StartQuarkQR(c.Request.Context())
	if err != nil {
		common.ErrorResp(c, err, 502)
		return
	}
	common.SuccessResp(c, result)
}

// BYOAQuarkStatus 查询一次扫码状态。
// 扫码成功后凭据只写入当前浏览器 HttpOnly Cookie，不返回给 JavaScript。
func BYOAQuarkStatus(c *gin.Context) {
	status, credential, err := byoa.CheckQuarkQR(c.Request.Context(), c.Query("token"))
	if err != nil {
		common.ErrorResp(c, err, 502)
		return
	}
	if status.Status == "success" {
		if err := common.SetBYOACredentialCookie(c, byoa.ProviderQuark, credential); err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
	}
	common.SuccessResp(c, status)
}

// BYOAAliyunStart 创建阿里云盘普通账号扫码二维码。
// ck/t 由浏览器持有，服务端不创建 Session。
func BYOAAliyunStart(c *gin.Context) {
	result, err := byoa.StartAliyunQR(c.Request.Context())
	if err != nil {
		common.ErrorResp(c, err, 502)
		return
	}
	common.SuccessResp(c, result)
}

// BYOAAliyunStatus 查询一次阿里扫码状态。
// 扫码成功后仅把短期 Access Token 写入当前浏览器 HttpOnly Cookie；
// 普通 Refresh Token 不持久化，也不返回给前端。
func BYOAAliyunStatus(c *gin.Context) {
	status, accessToken, err := byoa.CheckAliyunQR(c.Request.Context(), c.Query("ck"), c.Query("t"))
	if err != nil {
		common.ErrorResp(c, err, 502)
		return
	}
	if status.Status == "success" {
		if err := common.SetBYOACredentialCookie(c, byoa.ProviderAliyun, accessToken); err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
	}
	common.SuccessResp(c, status)
}

// BYOAClear 清除当前浏览器对应 Provider 的凭据，便于失效后重新扫码。
func BYOAClear(c *gin.Context) {
	provider := byoa.Provider(c.Query("provider"))
	if err := common.ClearBYOACredentialCookie(c, provider); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	common.SuccessResp(c)
}
