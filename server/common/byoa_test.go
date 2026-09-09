package common

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/byoa"
	"github.com/gin-gonic/gin"
)

func TestErrorRespConvertsBYOAAuthError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	ErrorResp(ctx, &byoa.AuthRequiredError{Provider: byoa.ProviderQuark}, 500)

	var resp Resp[BYOAAuthData]
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Code != 401 {
		t.Fatalf("code = %d, want 401", resp.Code)
	}
	if !resp.Data.BYOAAuthRequired {
		t.Fatal("byoa_auth_required = false, want true")
	}
	if resp.Data.Provider != byoa.ProviderQuark {
		t.Fatalf("provider = %q, want %q", resp.Data.Provider, byoa.ProviderQuark)
	}
}

func TestSetBYOACredentialCookieRejectsOversizedValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "https://example.test/", nil)

	err := SetBYOACredentialCookie(ctx, byoa.ProviderQuark, strings.Repeat("x", 4000))
	if err == nil {
		t.Fatal("expected oversized BYOA credential to be rejected")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("error = %q, want size diagnostic", err)
	}
	if got := recorder.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("Set-Cookie emitted for oversized credential: %v", got)
	}
}

func TestSetBYOACredentialCookieAttributes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "http://example.test/", nil)
	ctx.Request.Header.Set("X-Forwarded-Proto", "https")

	if err := SetBYOACredentialCookie(ctx, byoa.ProviderQuark, "test-quark-cookie"); err != nil {
		t.Fatalf("SetBYOACredentialCookie() error = %v", err)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != byoa.CookieQuark || cookie.Path != "/" {
		t.Fatalf("cookie name/path = %q/%q", cookie.Name, cookie.Path)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie attributes: HttpOnly=%t Secure=%t SameSite=%v", cookie.HttpOnly, cookie.Secure, cookie.SameSite)
	}
	if len(cookie.Value) == 0 || len(cookie.Value) > maxBYOACookieValueBytes {
		t.Fatalf("encrypted cookie bytes = %d", len(cookie.Value))
	}
}
