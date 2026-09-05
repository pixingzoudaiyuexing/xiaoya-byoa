package byoa

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func withAliyunGenerateEndpoint(t *testing.T, endpoint string) {
	t.Helper()
	old := aliyunQRGenerateEndpoint
	aliyunQRGenerateEndpoint = endpoint
	t.Cleanup(func() { aliyunQRGenerateEndpoint = old })
}

func requireAliyunStartErrorCode(t *testing.T, want string) error {
	t.Helper()
	_, err := StartAliyunQR(context.Background())
	if err == nil {
		t.Fatalf("expected Aliyun QR start error %q", want)
	}
	got, ok := AliyunQRStartErrorCode(err)
	if !ok {
		t.Fatalf("error %T is not a classified Aliyun QR start error", err)
	}
	if got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	return err
}

func TestAliyunQRStartNetworkErrorIsClassifiedAndSanitized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := server.URL
	server.Close()
	withAliyunGenerateEndpoint(t, endpoint)

	err := requireAliyunStartErrorCode(t, AliyunQRStartErrorNetwork)
	if got, want := err.Error(), "aliyun QR generate network error"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), endpoint) {
		t.Fatal("transport endpoint leaked into public error")
	}
}

func TestAliyunQRStartHTTPErrorCodes(t *testing.T) {
	tests := []struct {
		name   string
		status int
		code   string
	}{
		{name: "forbidden", status: http.StatusForbidden, code: AliyunQRStartErrorHTTP403},
		{name: "rate limited", status: http.StatusTooManyRequests, code: AliyunQRStartErrorHTTP429},
		{name: "server error", status: http.StatusServiceUnavailable, code: AliyunQRStartErrorHTTP5xx},
		{name: "other", status: http.StatusBadRequest, code: AliyunQRStartErrorHTTPOther},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"message":"sensitive-upstream-message"}`))
			}))
			defer server.Close()
			withAliyunGenerateEndpoint(t, server.URL)

			err := requireAliyunStartErrorCode(t, tt.code)
			if strings.Contains(err.Error(), "sensitive-upstream-message") {
				t.Fatal("upstream response body leaked into error")
			}
		})
	}
}

func TestAliyunQRStartResultAndInvalidResponseCodes(t *testing.T) {
	tests := []struct {
		name string
		body string
		code string
	}{
		{
			name: "known region result",
			body: `{"content":{"data":{"resultCode":100,"titleMsg":"sensitive-upstream-message"}}}`,
			code: AliyunQRStartErrorResult100,
		},
		{
			name: "other result",
			body: `{"content":{"data":{"resultCode":42,"titleMsg":"sensitive-upstream-message"}}}`,
			code: AliyunQRStartErrorResultOther,
		},
		{
			name: "invalid response",
			body: `{"content":{"data":{"resultCode":0,"titleMsg":"sensitive-upstream-message"}}}`,
			code: AliyunQRStartErrorInvalidResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			withAliyunGenerateEndpoint(t, server.URL)

			err := requireAliyunStartErrorCode(t, tt.code)
			if strings.Contains(err.Error(), "sensitive-upstream-message") {
				t.Fatal("upstream response message leaked into error")
			}
		})
	}
}
