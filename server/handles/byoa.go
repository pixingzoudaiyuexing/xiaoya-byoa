package handles

import (
	"sync"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/byoa"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type byoaRateEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type byoaIPRateLimiter struct {
	mu        sync.Mutex
	entries   map[string]*byoaRateEntry
	rate      rate.Limit
	burst     int
	lastSweep time.Time
}

func newBYOAIPRateLimiter(r rate.Limit, burst int) *byoaIPRateLimiter {
	return &byoaIPRateLimiter{
		entries:   make(map[string]*byoaRateEntry),
		rate:      r,
		burst:     burst,
		lastSweep: time.Now(),
	}
}

func (l *byoaIPRateLimiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastSweep) >= 5*time.Minute || len(l.entries) > 10000 {
		cutoff := now.Add(-10 * time.Minute)
		for key, entry := range l.entries {
			if entry.lastSeen.Before(cutoff) {
				delete(l.entries, key)
			}
		}
		l.lastSweep = now
	}

	entry := l.entries[ip]
	if entry == nil {
		entry = &byoaRateEntry{limiter: rate.NewLimiter(l.rate, l.burst)}
		l.entries[ip] = entry
	}
	entry.lastSeen = now
	return entry.limiter.AllowN(now, 1)
}

var (
	// 创建二维码访问第三方登录服务，限制更严格：持续约 1 次/秒，burst 5。
	byoaQRStartLimiter = newBYOAIPRateLimiter(rate.Every(time.Second), 5)
	// 前端默认每 2 秒轮询一次，状态接口允许持续约 5 次/秒，burst 20。
	byoaQRStatusLimiter = newBYOAIPRateLimiter(rate.Every(200*time.Millisecond), 20)
)

func allowBYOAQR(c *gin.Context, limiter *byoaIPRateLimiter) bool {
	ip := c.ClientIP()
	if ip == "" {
		ip = "unknown"
	}
	if limiter.allow(ip, time.Now()) {
		return true
	}
	common.ErrorStrResp(c, "扫码请求过于频繁，请稍后重试", 429)
	return false
}

// BYOAQuarkStart 创建夸克扫码二维码。
// token 由浏览器持有，服务端不创建 Session。
func BYOAQuarkStart(c *gin.Context) {
	if !allowBYOAQR(c, byoaQRStartLimiter) {
		return
	}
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
	if !allowBYOAQR(c, byoaQRStatusLimiter) {
		return
	}
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

func respondAliyunQRStartError(c *gin.Context, err error) bool {
	errorCode, ok := byoa.AliyunQRStartErrorCode(err)
	if !ok {
		return false
	}
	responseCode := 502
	if errorCode == byoa.AliyunQRStartErrorNetwork {
		// 本机到阿里上游的 transport 故障与有效 HTTP 上游错误分开，便于 CI/前端稳定判断。
		responseCode = 503
	}
	common.ErrorWithDataResp(c, err, responseCode, gin.H{"error_code": errorCode})
	return true
}

// BYOAAliyunStart 创建阿里云盘普通账号扫码二维码。
// ck/t 由浏览器持有，服务端不创建 Session。
func BYOAAliyunStart(c *gin.Context) {
	if !allowBYOAQR(c, byoaQRStartLimiter) {
		return
	}
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
	if !allowBYOAQR(c, byoaQRStatusLimiter) {
		return
	}
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
