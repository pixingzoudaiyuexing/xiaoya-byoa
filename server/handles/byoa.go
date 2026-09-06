package handles

import (
	"net/http"

	"github.com/OpenListTeam/OpenList/v4/internal/byoa"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
)

type byoaQuarkStatusReq struct {
	Token string `json:"token"`
}

type byoaAliyunStatusReq struct {
	CK string `json:"ck"`
	T  string `json:"t"`
}

func quarkStatusToken(c *gin.Context) (string, bool) {
	if c.Request.Method != http.MethodPost {
		// 临时兼容旧 CI/缓存前端；正式访客脚本只使用 POST JSON，避免 token 进入 URL/access log。
		return c.Query("token"), true
	}
	var req byoaQuarkStatusReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return "", false
	}
	return req.Token, true
}

func aliyunStatusParams(c *gin.Context) (ck, t string, ok bool) {
	if c.Request.Method != http.MethodPost {
		// 临时兼容旧 CI/缓存前端；正式访客脚本只使用 POST JSON，避免 ck/t 进入 URL/access log。
		return c.Query("ck"), c.Query("t"), true
	}
	var req byoaAliyunStatusReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return "", "", false
	}
	return req.CK, req.T, true
}

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
	token, ok := quarkStatusToken(c)
	if !ok {
		return
	}
	status, credential, err := byoa.CheckQuarkQR(c.Request.Context(), token)
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

func respondAliyunQRStartError(c *gin.Context, err error) bool {
	errorCode, ok := byoa.AliyunQRStartErrorCode(err)
	if !ok {
		return false
	}
	responseCode := 502
	data := gin.H{"error_code": errorCode}
	if errorCode == byoa.AliyunQRStartErrorNetwork {
		// 本机到阿里上游的 transport 故障与有效 HTTP 上游错误分开，便于 CI/前端稳定判断。
		responseCode = 503
		if transportClass, ok := byoa.AliyunQRStartTransportClass(err); ok {
			// 只返回固定 allowlist 类别，不返回底层 error 文本或任何请求/凭据内容。
			data["transport_class"] = transportClass
		}
	}
	if errorCode == byoa.AliyunQRStartErrorInvalidResponse {
		if responseClass, ok := byoa.AliyunQRStartResponseClass(err); ok {
			// 只返回响应的大类标签，不返回正文、URL、Content-Type 原文或任何凭据。
			data["response_class"] = responseClass
		}
	}
	common.ErrorWithDataResp(c, err, responseCode, data)
	return true
}

// BYOAAliyunStart 创建阿里云盘普通账号扫码二维码。
// ck/t 由浏览器持有，服务端不创建扫码 Session。
func BYOAAliyunStart(c *gin.Context) {
	result, err := byoa.StartAliyunQR(c.Request.Context())
	if err != nil {
		if !respondAliyunQRStartError(c, err) {
			common.ErrorResp(c, err, 502)
		}
		return
	}
	common.SuccessResp(c, result)
}

// BYOAAliyunStatus 查询一次阿里扫码状态。
// 扫码成功后仅把短期 Access Token 写入当前浏览器 HttpOnly Cookie；
// 普通 Refresh Token 不持久化，也不返回给前端。
func BYOAAliyunStatus(c *gin.Context) {
	ck, t, ok := aliyunStatusParams(c)
	if !ok {
		return
	}
	status, accessToken, err := byoa.CheckAliyunQR(c.Request.Context(), ck, t)
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
