package byoa

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Provider 表示 BYOA 模式下由访问者自行授权的网盘类型。
// MVP 只支持阿里云盘和夸克，其他网盘暂不接入。
type Provider string

const (
	ProviderAliyun Provider = "aliyun"
	ProviderQuark  Provider = "quark"
)

const (
	CookieAliyun = "xy_byoa_aliyun"
	CookieQuark  = "xy_byoa_quark"
)

var ErrUnsupportedProvider = errors.New("unsupported BYOA provider")

// AuthRequiredError 表示当前浏览器尚未提供指定网盘的凭据。
// 上层可将它转换为 NEED_AUTH(provider)，由前端弹出对应扫码入口。
type AuthRequiredError struct {
	Provider Provider
}

func (e *AuthRequiredError) Error() string {
	return fmt.Sprintf("BYOA_AUTH_REQUIRED:%s", e.Provider)
}

// IsAuthRequired 判断错误是否为 BYOA 授权缺失错误。
func IsAuthRequired(err error) bool {
	var target *AuthRequiredError
	return errors.As(err, &target)
}

// CookieName 返回指定 Provider 在当前浏览器中的凭据 Cookie 名称。
func CookieName(provider Provider) (string, error) {
	switch provider {
	case ProviderAliyun:
		return CookieAliyun, nil
	case ProviderQuark:
		return CookieQuark, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
}

// EncodeCredential 将上游网盘凭据转换为适合写入浏览器 Cookie 的值。
// 夸克 Cookie 本身包含分号等 Cookie 分隔字符，不能原样嵌套保存。
func EncodeCredential(value string) string {
	return url.QueryEscape(value)
}

// DecodeCredential 还原通过 EncodeCredential 写入浏览器 Cookie 的网盘凭据。
func DecodeCredential(value string) (string, error) {
	return url.QueryUnescape(value)
}

// CredentialFromHeader 从当前 HTTP 请求 Header 中读取指定网盘的 BYOA 凭据。
// 凭据只来自当前浏览器请求，不读取服务器全局账号或数据库 Session。
func CredentialFromHeader(header http.Header, provider Provider) (string, error) {
	cookieName, err := CookieName(provider)
	if err != nil {
		return "", err
	}

	req := &http.Request{Header: header}
	cookie, err := req.Cookie(cookieName)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return "", &AuthRequiredError{Provider: provider}
		}
		return "", err
	}

	credential, err := DecodeCredential(cookie.Value)
	if err != nil {
		return "", fmt.Errorf("decode BYOA credential for %s: %w", provider, err)
	}
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return "", &AuthRequiredError{Provider: provider}
	}

	return credential, nil
}
