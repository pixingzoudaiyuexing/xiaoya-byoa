package byoa

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAliyunQRGenerateResultCodeIsSafe(t *testing.T) {
	var seenUserAgent, seenReferer, seenAppName string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenUserAgent = r.Header.Get("User-Agent")
		seenReferer = r.Header.Get("Referer")
		seenAppName = r.URL.Query().Get("appName")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":{"data":{"resultCode":100,"codeContent":"","ck":"","t":"","titleMsg":"sensitive-upstream-message"}}}`))
	}))
	defer server.Close()

	oldEndpoint := aliyunQRGenerateEndpoint
	aliyunQRGenerateEndpoint = server.URL
	defer func() { aliyunQRGenerateEndpoint = oldEndpoint }()

	_, err := StartAliyunQR(context.Background())
	if err == nil {
		t.Fatal("expected Aliyun QR result-code error")
	}
	if got, want := err.Error(), "aliyun QR generate result code: 100"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), "sensitive-upstream-message") {
		t.Fatal("upstream response message leaked into error")
	}
	if seenAppName != "aliyun_drive" {
		t.Fatalf("appName = %q, want aliyun_drive", seenAppName)
	}
	if !strings.Contains(seenUserAgent, "Mozilla/5.0") {
		t.Fatalf("User-Agent = %q, want browser-like UA", seenUserAgent)
	}
	if seenReferer != aliyunBrowserReferer {
		t.Fatalf("Referer = %q, want %q", seenReferer, aliyunBrowserReferer)
	}
}

func TestClassifyAliyunTransportError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "none", err: nil, want: "none"},
		{name: "timeout", err: context.DeadlineExceeded, want: "timeout"},
		{name: "dns", err: &net.DNSError{Err: "no such host", Name: "example.invalid"}, want: "dns"},
		{name: "eof", err: io.EOF, want: "eof"},
		{name: "tls", err: errors.New("remote error: tls: handshake failure"), want: "tls"},
		{name: "reset", err: errors.New("read: connection reset by peer"), want: "reset"},
		{name: "refused", err: errors.New("dial tcp: connection refused"), want: "refused"},
		{name: "other", err: errors.New("opaque transport failure"), want: "other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyAliyunTransportError(tt.err); got != tt.want {
				t.Fatalf("classifyAliyunTransportError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAliyunRefreshTokenFromBizExt(t *testing.T) {
	payload := `{"pds_login_result":{"refreshToken":"refresh-token-test"}}`
	encoded := base64.StdEncoding.EncodeToString([]byte(payload))

	got, err := aliyunRefreshTokenFromBizExt(encoded)
	if err != nil {
		t.Fatalf("aliyunRefreshTokenFromBizExt() error = %v", err)
	}
	if got != "refresh-token-test" {
		t.Fatalf("refresh token = %q", got)
	}
}

func TestAliyunRefreshTokenFromBizExtMissingToken(t *testing.T) {
	payload := `{"pds_login_result":{}}`
	encoded := base64.StdEncoding.EncodeToString([]byte(payload))

	if _, err := aliyunRefreshTokenFromBizExt(encoded); err == nil {
		t.Fatal("expected missing refresh token error")
	}
}
