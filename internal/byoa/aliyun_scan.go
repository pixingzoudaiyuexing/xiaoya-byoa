package byoa

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/go-resty/resty/v2"
)

var (
	aliyunQRGenerateEndpoint = "https://passport.aliyundrive.com/newlogin/qrcode/generate.do"
	aliyunQRQueryEndpoint    = "https://passport.aliyundrive.com/newlogin/qrcode/query.do"
	aliyunRefreshEndpoint    = "https://auth.alipan.com/v2/account/token"
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

// StartAliyunQR 创建阿里云盘普通账号扫码二维码。
// ck/t 直接交由浏览器持有，服务端不维护扫码 Session。
func StartAliyunQR(ctx context.Context) (*AliyunQRStart, error) {
	var result aliyunGenerateResp
	resp, err := resty.New().R().
		SetContext(ctx).
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
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("aliyun QR generate http status: %d", resp.StatusCode())
	}
	data := result.Content.Data
	if data.CodeContent == "" || data.CK == "" || data.T == "" {
		return nil, errors.New("invalid aliyun QR response")
	}
	image, err := qrDataURI(data.CodeContent)
	if err != nil {
		return nil, err
	}
	return &AliyunQRStart{
		CK:      data.CK,
		T:       data.T,
		QRURL:   data.CodeContent,
		QRImage: image,
	}, nil
}

// CheckAliyunQR 查询一次阿里云盘扫码状态。
// 成功后只把短期 Access Token 返回给服务端 handler 写入 HttpOnly Cookie；
// Refresh Token 只在当前函数内用于换取 Access Token，不持久化。
func CheckAliyunQR(ctx context.Context, ck, t string) (status *AliyunQRStatus, accessToken string, err error) {
	ck = strings.TrimSpace(ck)
	t = strings.TrimSpace(t)
	if ck == "" || t == "" || len(ck) > 2048 || len(t) > 2048 {
		return nil, "", errors.New("invalid aliyun QR parameters")
	}

	var result aliyunQueryResp
	resp, err := resty.New().R().
		SetContext(ctx).
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
	resp, err := resty.New().R().
		SetContext(ctx).
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
