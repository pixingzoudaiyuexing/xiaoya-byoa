package byoa

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAliyunQRStartMalformedJSONIsInvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":`))
	}))
	defer server.Close()

	withAliyunGenerateEndpoint(t, server.URL)

	_, err := StartAliyunQR(context.Background())
	if err == nil {
		t.Fatal("expected malformed Aliyun JSON to fail")
	}
	code, ok := AliyunQRStartErrorCode(err)
	if !ok {
		t.Fatalf("expected classified Aliyun QR start error, got %T", err)
	}
	if code != AliyunQRStartErrorInvalidResponse {
		t.Fatalf("error code = %q, want %q", code, AliyunQRStartErrorInvalidResponse)
	}
}
