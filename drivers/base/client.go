package base

import (
	"context"
	"crypto/tls"
	stdnet "net"
	"net/http"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	internalnet "github.com/OpenListTeam/OpenList/v4/internal/net"
	"github.com/go-resty/resty/v2"
)

var (
	NoRedirectClient *resty.Client
	RestyClient      *resty.Client
	HttpClient       *http.Client
)

var DefaultTimeout = time.Second * 30

const UserAgent = "Mozilla/5.0 (Macintosh; Apple macOS 26_1_0) AppleWebKit/537.36 (KHTML, like Gecko) Safari/537.36 Chrome/142.0.0.0 OpenList/425.6.30"
const UserAgentNT = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Safari/537.36 Chrome/142.0.0.0 OpenList/425.6.30"

const alipanAPIHost = "api.alipan.com"

func InitClient() {
	NoRedirectClient = resty.New().SetRedirectPolicy(
		resty.RedirectPolicyFunc(func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}),
	).SetTLSClientConfig(&tls.Config{InsecureSkipVerify: conf.Conf.TlsInsecureSkipVerify})
	NoRedirectClient.SetHeader("user-agent", UserAgent)
	internalnet.SetRestyProxyIfConfigured(NoRedirectClient)

	RestyClient = NewRestyClient()
	HttpClient = internalnet.NewHttpClient()
}

func NewRestyClient() *resty.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: conf.Conf.TlsInsecureSkipVerify}
	dialer := &stdnet.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport.DialContext = func(ctx context.Context, network, address string) (stdnet.Conn, error) {
		return dialer.DialContext(ctx, outboundNetwork(network, address), address)
	}

	client := resty.New().
		SetTransport(transport).
		SetHeader("user-agent", UserAgent).
		SetRetryCount(3).
		SetRetryResetReaders(true).
		SetTimeout(DefaultTimeout)

	internalnet.SetRestyProxyIfConfigured(client)
	return client
}

// outboundNetwork 只对阿里公开分享 API 固定 IPv4。
// 部分 VPS 能解析 api.alipan.com 的 AAAA，但 IPv6 TLS 握手不可用；若默认客户端先选中 IPv6，
// 会等到 TLS handshake timeout，导致目录浏览非常慢。其它目标继续保持 Go 默认双栈行为。
func outboundNetwork(network, address string) string {
	host, _, err := stdnet.SplitHostPort(address)
	if err == nil && strings.EqualFold(host, alipanAPIHost) {
		return "tcp4"
	}
	return network
}
