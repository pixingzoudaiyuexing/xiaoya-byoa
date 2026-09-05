package byoa

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAliyunQRGenerateResultCodeIsSafe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
