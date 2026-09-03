package byoa

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/qr"
	"github.com/go-resty/resty/v2"
	"github.com/google/uuid"
)

const quarkClientID = "532"

const quarkUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) quark-cloud-drive/2.5.20 Chrome/100.0.4896.160 Electron/18.3.5.4-b478491100 Safari/537.36 Channel/pckk_other_ch"

var (
	quarkTokenEndpoint  = "https://uop.quark.cn/cas/ajax/getTokenForQrcodeLogin"
	quarkStatusEndpoint = "https://uop.quark.cn/cas/ajax/getServiceTicketByQrcodeToken"
	quarkAccountURL     = "https://pan.quark.cn/account/info"
	quarkConfigURL      = "https://drive-pc.quark.cn/1/clouddrive/config?pr=ucpro&fr=pc&uc_param_str="
)

type QuarkQRStart struct {
	Token   string `json:"token"`
	QRURL   string `json:"qr_url"`
	QRImage string `json:"qr_image"`
}

type QuarkQRStatus struct {
	Status string `json:"status"`
}

type quarkQRTokenResp struct {
	Status int `json:"status"`
	Data   struct {
		Members struct {
			Token string `json:"token"`
		} `json:"members"`
	} `json:"data"`
}

type quarkQRStatusResp struct {
	Status int `json:"status"`
	Data   struct {
		Members struct {
			ServiceTicket string `json:"service_ticket"`
		} `json:"members"`
	} `json:"data"`
}

// StartQuarkQR 创建一个完全无服务端 Session 的夸克扫码任务。
// token 直接交给浏览器保存并用于后续轮询，服务端不维护扫码状态表。
func StartQuarkQR(ctx context.Context) (*QuarkQRStart, error) {
	var result quarkQRTokenResp
	resp, err := newBYOAHTTPClient().R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"client_id":  quarkClientID,
			"v":          "1.2",
			"request_id": uuid.NewString(),
		}).
		SetResult(&result).
		Get(quarkTokenEndpoint)
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("quark QR token http status: %d", resp.StatusCode())
	}
	if result.Data.Members.Token == "" {
		return nil, errors.New("empty quark QR token")
	}

	token := result.Data.Members.Token
	qrURL := "https://su.quark.cn/4_eMHBJ?token=" + url.QueryEscape(token) + "&client_id=532&ssb=weblogin&uc_param_str=&uc_biz_str=S%3Acustom%7COPT%3ASAREA%400%7COPT%3AIMMERSIVE%401%7COPT%3ABACK_BTN_STYLE%400"
	qrImage, err := qrDataURI(qrURL)
	if err != nil {
		return nil, err
	}

	return &QuarkQRStart{
		Token:   token,
		QRURL:   qrURL,
		QRImage: qrImage,
	}, nil
}

// CheckQuarkQR 查询一次扫码状态。
// credential 仅在扫码成功时返回给服务端调用者，用于写 HttpOnly Cookie；API 不应把它放进 JSON。
func CheckQuarkQR(ctx context.Context, token string) (status *QuarkQRStatus, credential string, err error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 1024 {
		return nil, "", errors.New("invalid quark QR token")
	}

	var result quarkQRStatusResp
	resp, err := newBYOAHTTPClient().R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"client_id":  quarkClientID,
			"v":          "1.2",
			"token":      token,
			"request_id": uuid.NewString(),
		}).
		SetResult(&result).
		Get(quarkStatusEndpoint)
	if err != nil {
		return nil, "", err
	}
	if resp.IsError() {
		return nil, "", fmt.Errorf("quark QR status http status: %d", resp.StatusCode())
	}

	switch result.Status {
	case 50004001:
		return &QuarkQRStatus{Status: "pending"}, "", nil
	case 50004002:
		return &QuarkQRStatus{Status: "expired"}, "", nil
	case 2000000:
		serviceTicket := result.Data.Members.ServiceTicket
		if serviceTicket == "" {
			return nil, "", errors.New("empty quark service ticket")
		}
		cookieValue, err := exchangeQuarkServiceTicket(ctx, serviceTicket)
		if err != nil {
			return nil, "", err
		}
		return &QuarkQRStatus{Status: "success"}, cookieValue, nil
	default:
		return nil, "", fmt.Errorf("unexpected quark QR status: %d", result.Status)
	}
}

func exchangeQuarkServiceTicket(ctx context.Context, serviceTicket string) (string, error) {
	client := newBYOAHTTPClient()
	accountResp, err := client.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"st": serviceTicket,
			"lw": "scan",
		}).
		Get(quarkAccountURL)
	if err != nil {
		return "", err
	}
	if accountResp.IsError() {
		return "", fmt.Errorf("quark account exchange http status: %d", accountResp.StatusCode())
	}

	cookies := cookieMap(accountResp.Cookies())
	if len(cookies) == 0 {
		return "", errors.New("empty quark account cookie")
	}

	cookieValue := cookieMapString(cookies)
	configResp, err := client.R().
		SetContext(ctx).
		SetHeaders(map[string]string{
			"User-Agent": quarkUserAgent,
			"Referer":    "https://pan.quark.cn",
			"Cookie":     cookieValue,
		}).
		Get(quarkConfigURL)
	if err != nil {
		return "", err
	}
	if configResp.IsError() {
		return "", fmt.Errorf("quark config http status: %d", configResp.StatusCode())
	}
	for name, value := range cookieMap(configResp.Cookies()) {
		cookies[name] = value
	}

	cookieValue = cookieMapString(cookies)
	if cookieValue == "" {
		return "", errors.New("empty quark final cookie")
	}
	return cookieValue, nil
}

func cookieMap(cookies []*http.Cookie) map[string]string {
	values := make(map[string]string, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil || cookie.Name == "" {
			continue
		}
		values[cookie.Name] = cookie.Value
	}
	return values
}

func cookieMapString(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+values[name])
	}
	return strings.Join(parts, "; ")
}

func qrDataURI(value string) (string, error) {
	code, err := qr.Encode(value, qr.M, qr.Auto)
	if err != nil {
		return "", err
	}
	code, err = barcode.Scale(code, 320, 320)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, code); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
