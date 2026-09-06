package base

import (
	"crypto/tls"
	"net/http"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	internalnet "github.com/OpenListTeam/OpenList/v4/internal/net"
	"github.com/go-resty/resty/v2"
)

var (
	NoRedirectClient  *resty.Client
	RestyClient       *resty.Client
	AliyunRestyClient *resty.Client
	HttpClient        *http.Client
)

var DefaultTimeout = time.Second * 30

const UserAgent = "Mozilla/5.0 (Macintosh; Apple macOS 26_1_0) AppleWebKit/537.36 (KHTML, like Gecko) Safari/537.36 Chrome/142.0.0.0 OpenList/425.6.30"
const UserAgentNT = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Safari/537.36 Chrome/142.0.0.0 OpenList/425.6.30"

func InitClient() {
	NoRedirectClient = resty.New().SetRedirectPolicy(
		resty.RedirectPolicyFunc(func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}),
	).SetTLSClientConfig(&tls.Config{InsecureSkipVerify: conf.Conf.TlsInsecureSkipVerify})
	NoRedirectClient.SetHeader("user-agent", UserAgent)
	internalnet.SetRestyProxyIfConfigured(NoRedirectClient)

	RestyClient = NewRestyClient()
	AliyunRestyClient = NewAliyunRestyClient()
	HttpClient = internalnet.NewHttpClient()
}

func NewRestyClient() *resty.Client {
	client := resty.New().
		SetTLSClientConfig(&tls.Config{InsecureSkipVerify: conf.Conf.TlsInsecureSkipVerify}).
		SetHeader("user-agent", UserAgent).
		SetRetryCount(3).
		SetRetryResetReaders(true).
		SetTimeout(DefaultTimeout)

	internalnet.SetRestyProxyIfConfigured(client)
	return client
}

// NewAliyunRestyClient 仅用于阿里公开分享 API。
// 某些 VPS 到阿里边缘节点存在“TCP 已连接但 TLS 握手超时”的路径问题；仅固定 tcp4 仍会黏在坏 IPv4 上。
// 这里在多个 IPv4 A 记录之间做 TLS 级 fallback，并使用 HTTP/1.1，避免影响其他网盘驱动。
func NewAliyunRestyClient() *resty.Client {
	transport := internalnet.NewIPv4TLSFallbackTransport(
		&tls.Config{InsecureSkipVerify: conf.Conf.TlsInsecureSkipVerify},
		4*time.Second,
		true,
	)
	transport.ResponseHeaderTimeout = 8 * time.Second

	client := resty.New().
		SetTransport(transport).
		SetHeader("user-agent", UserAgent).
		SetTimeout(DefaultTimeout)
	internalnet.SetRestyProxyIfConfigured(client)
	return client
}

// GetAliyunRestyClient 允许单测在未执行 InitClient 时安全取得专用客户端。
func GetAliyunRestyClient() *resty.Client {
	if AliyunRestyClient != nil {
		return AliyunRestyClient
	}
	return NewAliyunRestyClient()
}
