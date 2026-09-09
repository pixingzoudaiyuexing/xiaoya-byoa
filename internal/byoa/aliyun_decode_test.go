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
	responseClass, ok := AliyunQRStartResponseClass(err)
	if !ok {
		t.Fatal("expected safe response class")
	}
	if responseClass != "json-decode" {
		t.Fatalf("response class = %q, want json-decode", responseClass)
	}
}

func TestClassifyAliyunInvalidResponseBody(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want string
	}{
		{name: "empty", body: []byte("  \n"), want: "empty"},
		{name: "html", body: []byte("<!doctype html><html></html>"), want: "html"},
		{name: "json", body: []byte(`{"broken":`), want: "json-decode"},
		{name: "text", body: []byte("upstream challenge"), want: "text"},
		{name: "binary", body: []byte{0xff, 0xfe, 0xfd}, want: "binary"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyAliyunInvalidResponseBody(tt.body); got != tt.want {
				t.Fatalf("class = %q, want %q", got, tt.want)
			}
		})
	}
}
