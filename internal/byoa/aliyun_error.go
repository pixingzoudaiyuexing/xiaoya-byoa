package byoa

import (
	"errors"
	"fmt"
)

const (
	AliyunQRStartErrorNetwork         = "aliyun_network"
	AliyunQRStartErrorHTTP403         = "aliyun_http_403"
	AliyunQRStartErrorHTTP429         = "aliyun_http_429"
	AliyunQRStartErrorHTTP5xx         = "aliyun_http_5xx"
	AliyunQRStartErrorHTTPOther       = "aliyun_http_other"
	AliyunQRStartErrorResult100       = "aliyun_result_code_100"
	AliyunQRStartErrorResultOther     = "aliyun_result_code_other"
	AliyunQRStartErrorInvalidResponse = "aliyun_invalid_response"
	AliyunQRStartErrorQREncode        = "aliyun_qr_encode"
)

type aliyunQRStartError struct {
	code           string
	message        string
	transportClass string
	responseClass  string
}

func (e *aliyunQRStartError) Error() string {
	return e.message
}

func sanitizeAliyunTransportClass(class string) string {
	switch class {
	case "timeout", "dns", "eof", "tls", "reset", "refused", "other":
		return class
	default:
		return "other"
	}
}

func sanitizeAliyunResponseClass(class string) string {
	switch class {
	case "empty", "html", "json-decode", "json-shape", "text", "binary", "other":
		return class
	default:
		return "other"
	}
}

func newAliyunQRStartNetworkError(transportClass string) error {
	// 不包装底层 transport error，避免 URL、代理信息或其他运行环境细节被错误链泄露。
	// 只保留固定 allowlist 类别用于真实环境诊断。
	return &aliyunQRStartError{
		code:           AliyunQRStartErrorNetwork,
		message:        "aliyun QR generate network error",
		transportClass: sanitizeAliyunTransportClass(transportClass),
	}
}

func newAliyunQRStartHTTPError(status int) error {
	code := AliyunQRStartErrorHTTPOther
	switch {
	case status == 403:
		code = AliyunQRStartErrorHTTP403
	case status == 429:
		code = AliyunQRStartErrorHTTP429
	case status >= 500 && status <= 599:
		code = AliyunQRStartErrorHTTP5xx
	}
	return &aliyunQRStartError{
		code:    code,
		message: fmt.Sprintf("aliyun QR generate http status: %d", status),
	}
}

func newAliyunQRStartResultError(resultCode int) error {
	code := AliyunQRStartErrorResultOther
	if resultCode == 100 {
		code = AliyunQRStartErrorResult100
	}
	return &aliyunQRStartError{
		code:    code,
		message: fmt.Sprintf("aliyun QR generate result code: %d", resultCode),
	}
}

func newAliyunQRStartInvalidResponseError(responseClass string) error {
	return &aliyunQRStartError{
		code:          AliyunQRStartErrorInvalidResponse,
		message:       "invalid aliyun QR response",
		responseClass: sanitizeAliyunResponseClass(responseClass),
	}
}

func newAliyunQRStartQREncodeError() error {
	return &aliyunQRStartError{
		code:    AliyunQRStartErrorQREncode,
		message: "invalid aliyun QR image payload",
	}
}

// AliyunQRStartErrorCode 返回可暴露给前端/CI 的固定错误码。
// 错误码不包含上游响应正文、二维码内容、ck/t 或任何凭据。
func AliyunQRStartErrorCode(err error) (string, bool) {
	var target *aliyunQRStartError
	if !errors.As(err, &target) {
		return "", false
	}
	return target.code, true
}

// AliyunQRStartTransportClass 只为 network 错误返回固定 allowlist transport 类别。
// 不暴露底层 error 文本、URL、代理、响应正文或任何凭据。
func AliyunQRStartTransportClass(err error) (string, bool) {
	var target *aliyunQRStartError
	if !errors.As(err, &target) || target.code != AliyunQRStartErrorNetwork || target.transportClass == "" {
		return "", false
	}
	return sanitizeAliyunTransportClass(target.transportClass), true
}

// AliyunQRStartResponseClass 只为 invalid_response 返回固定 allowlist 响应类型。
// 只描述响应的大类，不返回 Content-Type 原文、正文、长度、URL 或任何凭据。
func AliyunQRStartResponseClass(err error) (string, bool) {
	var target *aliyunQRStartError
	if !errors.As(err, &target) || target.code != AliyunQRStartErrorInvalidResponse || target.responseClass == "" {
		return "", false
	}
	return sanitizeAliyunResponseClass(target.responseClass), true
}
