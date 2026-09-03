package common

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/byoa"
	"github.com/gin-gonic/gin"
)

// 浏览器通常把单个 Cookie 限制在约 4 KiB。预留名称和属性空间，避免服务端返回成功但浏览器静默丢弃凭据。
const maxBYOACookieValueBytes = 3500

// SetBYOACredentialCookie 将网盘凭据加密后只写入当前浏览器的 HttpOnly Cookie。
// 不写数据库、不写 Storage，也不把凭据返回给前端 JavaScript。
func SetBYOACredentialCookie(c *gin.Context, provider byoa.Provider, credential string) error {
	name, err := byoa.CookieName(provider)
	if err != nil {
		return err
	}
	encoded, err := byoa.EncodeCredential(provider, credential)
	if err != nil {
		return err
	}
	if len(encoded) > maxBYOACookieValueBytes {
		return fmt.Errorf("BYOA %s credential cookie too large: %d bytes", provider, len(encoded))
	}

	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    encoded,
		Path:     "/",
		MaxAge:   int((7 * 24 * time.Hour).Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// ClearBYOACredentialCookie 清除当前浏览器指定 Provider 的凭据。
func ClearBYOACredentialCookie(c *gin.Context, provider byoa.Provider) error {
	name, err := byoa.CookieName(provider)
	if err != nil {
		return err
	}
	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}
