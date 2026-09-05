package byoa

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
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

const (
	credentialKeyEnv        = "BYOA_COOKIE_KEY"
	credentialEncodingV1    = "v1."
	credentialEncryptionKey = 32
)

var (
	ErrUnsupportedProvider = errors.New("unsupported BYOA provider")
	credentialKeyOnce      sync.Once
	credentialKeyValue     []byte
	credentialKeyErr       error
)

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

// EncodeCredential 使用 AES-256-GCM 加密浏览器凭据，再编码成 Cookie 安全的 base64url。
// Provider 会作为 AEAD 附加认证数据，防止把阿里 Cookie 密文误换到夸克 Cookie 中复用。
func EncodeCredential(provider Provider, value string) (string, error) {
	if _, err := CookieName(provider); err != nil {
		return "", err
	}
	key, err := credentialKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create BYOA credential cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create BYOA credential AEAD: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate BYOA credential nonce: %w", err)
	}
	payload := aead.Seal(nonce, nonce, []byte(value), credentialAAD(provider))
	return credentialEncodingV1 + base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecodeCredential 解密通过 EncodeCredential 写入浏览器 Cookie 的网盘凭据。
// 篡改、换 Provider 或使用错误密钥都会被 AES-GCM 认证直接拒绝。
func DecodeCredential(provider Provider, value string) (string, error) {
	if _, err := CookieName(provider); err != nil {
		return "", err
	}
	if !strings.HasPrefix(value, credentialEncodingV1) {
		return "", errors.New("unsupported BYOA credential encoding")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, credentialEncodingV1))
	if err != nil {
		return "", fmt.Errorf("decode BYOA credential payload: %w", err)
	}
	key, err := credentialKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create BYOA credential cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create BYOA credential AEAD: %w", err)
	}
	if len(payload) < aead.NonceSize()+aead.Overhead() {
		return "", errors.New("invalid BYOA credential payload")
	}
	nonce := payload[:aead.NonceSize()]
	ciphertext := payload[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, credentialAAD(provider))
	if err != nil {
		return "", fmt.Errorf("authenticate BYOA credential: %w", err)
	}
	return string(plaintext), nil
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

	credential, err := DecodeCredential(provider, cookie.Value)
	if err != nil {
		// Cookie 损坏、密钥轮换或旧明文格式都按“需要重新授权”处理，
		// 不把密码学错误暴露给访客，也不会形成 500。
		return "", &AuthRequiredError{Provider: provider}
	}
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return "", &AuthRequiredError{Provider: provider}
	}

	return credential, nil
}

func credentialAAD(provider Provider) []byte {
	return []byte("xiaoya-byoa/credential/" + string(provider))
}

// credentialKey 优先使用容器 entrypoint 从持久化 data volume 注入的 BYOA_COOKIE_KEY。
// 非 Docker/单元测试环境没有配置时只生成进程内临时密钥；重启后旧 Cookie 会要求重新扫码。
func credentialKey() ([]byte, error) {
	credentialKeyOnce.Do(func() {
		raw := strings.TrimSpace(os.Getenv(credentialKeyEnv))
		if raw != "" {
			credentialKeyValue, credentialKeyErr = decodeCredentialKey(raw)
			return
		}
		credentialKeyValue = make([]byte, credentialEncryptionKey)
		if _, err := rand.Read(credentialKeyValue); err != nil {
			credentialKeyErr = fmt.Errorf("generate ephemeral BYOA cookie key: %w", err)
		}
	})
	if credentialKeyErr != nil {
		return nil, credentialKeyErr
	}
	if len(credentialKeyValue) != credentialEncryptionKey {
		return nil, fmt.Errorf("invalid BYOA cookie key length: %d", len(credentialKeyValue))
	}
	return credentialKeyValue, nil
}

func decodeCredentialKey(raw string) ([]byte, error) {
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(raw)
		if err == nil && len(decoded) == credentialEncryptionKey {
			return decoded, nil
		}
	}
	return nil, errors.New("BYOA_COOKIE_KEY must be base64 for exactly 32 bytes")
}
