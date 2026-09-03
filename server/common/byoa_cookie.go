package common

import (
	"net/http"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/byoa"
	"github.com/gin-gonic/gin"
)

// SetBYOACredentialCookie 将网盘凭据只写入当前浏览器的 HttpOnly Cookie。
// 不写数据库、不写 Storage，也不把凭据返回给前端 JavaScript。
func SetBYOACredentialCookie(c *gin.Context, provider byoa.Provider, credential string) error {
	name, err := byoa.CookieName(provider)
	if err != nil {
		return err
	}

	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    byoa.EncodeCredential(credential),
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
