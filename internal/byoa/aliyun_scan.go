package byoa

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

var (
	aliyunQRGenerateEndpoint = "https://passport.aliyundrive.com/newlogin/qrcode/generate.do"
	aliyunQRQueryEndpoint    = "https://passport.aliyundrive.com/newlogin/qrcode/query.do"
	aliyunRefreshEndpoint    = "https://auth.alipan.com/v2/account/token"
)

const (
	byoaUpstreamTimeout     = 15 * time.Second
	aliyunBrowserUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36"
	aliyunBrowserReferer    = "https://www.aliyundrive.com/"
)

type AliyunQRStart struct {
	CK      string `json:"ck"`
	T       string `json:"t"`
	QRURL   string `json:"qr_url"`
	QRImage string `json:"qr_image"`
}

type AliyunQRStatus struct {
	Status string `json:"status"`
}

type aliyunGenerateResp struct {
	Content struct {
		Data struct {
			ResultCode  int    `json:"resultCode"`
			CodeContent string `json:"codeContent"`
			CK          string `json:"ck"`
			T           string `json:"t"`
		} `json:"data"`
	} `json:"content"`
}

type aliyunQueryResp struct {
	Content struct {
		Data struct {
			QRCodeStatus string `json:"qrCodeStatus"`
			BizExt       string `json:"bizExt"`
		} `json:"data"`
	} `json:"content"`
}

type aliyunLoginBizExt struct {
	LoginResult struct {
		RefreshToken string `json:"refreshToken"`
	} `json:"pds_login_result"`
}

type aliyunRefreshResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Code         string `json:"code"`
	Message      string `json:"message"`
}

func newBYOAHTTPClient() *resty.Client {
	return resty.New().SetTimeout(byoaUpstreamTimeout)
}

// newAliyunHTTPClient 只影响阿里账号授权链路。
// 真实 VPS 已验证阿里 IPv4 可用而 IPv6 不可达；另外对扫码登录使用 HTTP/1.1，避免部分阿里边缘节点
// 与 Go HTTP/2 客户端组合出现长时间 transport timeout。该限制不影响 Quark、OpenList 其他驱动或播放链路。
func newAliyunHTTPClient() *resty.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport.DialContext = func(ctx context.Context, _ string, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, "tcp4", address)
	}
	transport.ForceAttemptHTTP2 = false
	transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	transport.TLSHandshakeTimeout = 8 * time.Second
	transport.ResponseHeaderTimeout = 8 * time.Second
	return resty.New().SetTransport(transport).SetTimeout(byoaUpstreamTimeout)
}

// classifyAliyunTransportError 只返回固定类别，禁止把底层 error 文本、URL、代理信息或凭据写入日志。
func classifyAliyunTransportError(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return "eof"
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "tls") || strings.Contains(lower, "x509"):
		return "tls"
	case strings.Contains(lower, "connection reset"):
		return "reset"
	case strings.Contains(lower, "connection refused"):
		return "refused"
	default:
		return "other"
	}
}

func aliyunBrowserHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      aliyunBrowserUserAgent,
		"Referer":         aliyunBrowserReferer,
		"Accept":          "application/json, text/plain, */*",
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
	}
}

// StartAliyunQR 创建阿里云盘普通账号扫码二维码。
// ck/t 直接交由浏览器持有，服务端不维护扫码 Session。
func StartAliyunQR(ctx context.Context) (*AliyunQRStart, error) {
	client := newAliyunHTTPClient()
	var result aliyunGenerateResp
	var resp *resty.Response
	var err error

	// 只对 transport 失败做一次短重试；有效 HTTP/WAF 响应绝不重试，保留明确错误分类。
	for attempt := 0; attempt < 2; attempt++ {
		result = aliyunGenerateResp{}
		resp, err = client.R().
			SetContext(ctx).
			SetHeaders(aliyunBrowserHeaders()).
			SetQueryParams(map[string]string{
				"appName":     "aliyun_drive",
				"fromSite":    "52",
				"appEntrance": "web",
				"isMobile":    "false",
				"lang":        "zh_CN",
				"returnUrl":   "",
				"bizParams":   "",
				"_bx-v":       "2.0.31",
			}).
			SetResult(&result).
			Get(aliyunQRGenerateEndpoint)
		if err == nil {
			break
		}
		log.Warnf("[BYOA][Aliyun] QR generate transport failure class=%s attempt=%d", classifyAliyunTransportError(err), attempt+1)
		if attempt == 0 {
			select {
			case <-ctx.Done():
				return nil, newAliyunQRStartNetworkError()
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	if err != nil {
		return nil, newAliyunQRStartNetworkError()
	}
	if resp.IsError() {
		return nil, newAliyunQRStartHTTPError(resp.StatusCode())
	}
	data := result.Content.Data
	if data.CodeContent == "" || data.CK == "" || data.T == "" {
		// 只暴露上游数字结果码用于诊断，绝不把响应正文、ck/t 或二维码内容写入错误。
		if data.ResultCode != 0 {
			return nil, newAliyunQRStartResultError(data.ResultCode)
		}
		return nil, newAliyunQRStartInvalidResponseError()
	}
	image, err := qrDataURI(data.CodeContent)
	if err != nil {
		return nil, newAliyunQRStartQREncodeError()
	}
	return &AliyunQRStart{
		CK:      data.CK,
		T:       data.T,
		QRURL:   data.CodeContent,
		QRImage: image,
	}, nil
}

// CheckAliyunQR 查询一次扫码状态。
// 成功后只把短期 Access Token 返回给服务端 handler 写入 HttpOnly Cookie；
// Refresh Token 只在当前函数内用于换取 Access Token，不持久化。
func CheckAliyunQR(ctx context.Context, ck, t string) (status *AliyunQRStatus, accessToken string, err error) {
	ck = strings.TrimSpace(ck)
	t = strings.TrimSpace(t)
	if ck == "" || t == "" || len(ck) > 2048 || len(t) > 2048 {
		return nil, "", errors.New("invalid aliyun QR parameters")
	}

	var result aliyunQueryResp
	headers := aliyunBrowserHeaders()
	headers["Origin"] = "https://www.aliyundrive.com"
	resp, err := newAliyunHTTPClient().R().
		SetContext(ctx).
		SetHeaders(headers).
		SetQueryParams(map[string]string{
			"appName":  "aliyun_drive",
			"fromSite": "52",
			"_bx-v":    "2.0.31",
		}).
		SetFormData(map[string]string{
			"t":           t,
			"ck":          ck,
			"appName":     "aliyun_drive",
			"appEntrance": "web",
			"isMobile":    "false",
			"lang":        "zh_CN",
			"returnUrl":   "",
			"fromSite":    "52",
			"bizParams":   "",
			"navlanguage": "zh-CN",
			"navPlatform": "MacIntel",
		}).
		SetResult(&result).
		Post(aliyunQRQueryEndpoint)
	if err != nil {
		return nil, "", err
	}
	if resp.IsError() {
		return nil, "", fmt.Errorf("aliyun QR query http status: %d", resp.StatusCode())
	}

	data := result.Content.Data
	switch data.QRCodeStatus {
	case "NEW":
		return &AliyunQRStatus{Status: "pending"}, "", nil
	case "SCANED":
		return &AliyunQRStatus{Status: "scanned"}, "", nil
	case "EXPIRED":
		return &AliyunQRStatus{Status: "expired"}, "", nil
	case "CANCELED":
		return &AliyunQRStatus{Status: "canceled"}, "", nil
	case "CONFIRMED":
		refreshToken, err := aliyunRefreshTokenFromBizExt(data.BizExt)
		if err != nil {
			return nil, "", err
		}
		accessToken, err := exchangeAliyunRefreshToken(ctx, refreshToken)
		if err != nil {
			return nil, "", err
		}
		return &AliyunQRStatus{Status: "success"}, accessToken, nil
	default:
		return nil, "", fmt.Errorf("unexpected aliyun QR status: %q", data.QRCodeStatus)
	}
}

func aliyunRefreshTokenFromBizExt(encoded string) (string, error) {
	if encoded == "" {
		return "", errors.New("empty aliyun bizExt")
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		// 某些响应省略 base64 padding，兼容 RawStdEncoding。
		decoded, err = base64.RawStdEncoding.DecodeString(encoded)
		if err != nil {
			return "", fmt.Errorf("decode aliyun bizExt: %w", err)
		}
	}
	var ext aliyunLoginBizExt
	if err := json.Unmarshal(decoded, &ext); err != nil {
		return "", fmt.Errorf("decode aliyun login result: %w", err)
	}
	refreshToken := strings.TrimSpace(ext.LoginResult.RefreshToken)
	if refreshToken == "" {
		return "", errors.New("aliyun refresh token not found")
	}
	return refreshToken, nil
}

func exchangeAliyunRefreshToken(ctx context.Context, refreshToken string) (string, error) {
	var result aliyunRefreshResp
	resp, err := newAliyunHTTPClient().R().
		SetContext(ctx).
		SetHeaders(aliyunBrowserHeaders()).
		SetBody(map[string]string{
			"refresh_token": refreshToken,
			"grant_type":    "refresh_token",
		}).
		SetResult(&result).
		SetError(&result).
		Post(aliyunRefreshEndpoint)
	if err != nil {
		return "", err
	}
	if resp.IsError() || result.Code != "" {
		if result.Message != "" {
			return "", errors.New(result.Message)
		}
		return "", fmt.Errorf("aliyun token refresh http status: %d", resp.StatusCode())
	}
	if result.AccessToken == "" {
		return "", errors.New("empty aliyun access token")
	}
	return result.AccessToken, nil
}
