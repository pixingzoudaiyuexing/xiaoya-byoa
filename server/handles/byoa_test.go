package handles

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBYOAStatusParamsRejectQueryStrings(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("quark", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/?token=test-token", nil)
		if token, ok := quarkStatusToken(ctx); ok || token != "" {
			t.Fatalf("query token accepted: token=%q ok=%t", token, ok)
		}
		var response struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.Code != http.StatusBadRequest {
			t.Fatalf("response code = %d, decode error = %v", response.Code, err)
		}
	})

	t.Run("aliyun", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/?ck=test-ck&t=test-t", nil)
		if ck, timestamp, ok := aliyunStatusParams(ctx); ok || ck != "" || timestamp != "" {
			t.Fatalf("query params accepted: ck=%q t=%q ok=%t", ck, timestamp, ok)
		}
		var response struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.Code != http.StatusBadRequest {
			t.Fatalf("response code = %d, decode error = %v", response.Code, err)
		}
	})
}

func TestBYOAStatusParamsAcceptPOSTJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("quark", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"token":"test-token"}`))
		ctx.Request.Header.Set("Content-Type", "application/json")
		if token, ok := quarkStatusToken(ctx); !ok || token != "test-token" {
			t.Fatalf("POST token: token=%q ok=%t", token, ok)
		}
	})

	t.Run("aliyun", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"ck":"test-ck","t":"test-t"}`))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ck, timestamp, ok := aliyunStatusParams(ctx)
		if !ok || ck != "test-ck" || timestamp != "test-t" {
			t.Fatalf("POST params: ck=%q t=%q ok=%t", ck, timestamp, ok)
		}
	})
}
