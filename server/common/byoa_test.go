package common

import (
	"encoding/json"
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
