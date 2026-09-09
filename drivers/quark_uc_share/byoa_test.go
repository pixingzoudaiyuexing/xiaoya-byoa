package quark_uc_share

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/byoa"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

func TestQuarkBYOARequiresBrowserCredential(t *testing.T) {
	d := &QuarkUCShare{}

	_, err := d.Link(context.Background(), nil, model.LinkArgs{})
	if err == nil {
		t.Fatal("Link() expected BYOA auth error")
	}
	if !byoa.IsAuthRequired(err) {
		t.Fatalf("Link() error = %T %v, want AuthRequiredError", err, err)
	}

	var authErr *byoa.AuthRequiredError
	if !errors.As(err, &authErr) {
		t.Fatalf("errors.As() failed for %T", err)
	}
	if authErr.Provider != byoa.ProviderQuark {
		t.Fatalf("provider = %q, want %q", authErr.Provider, byoa.ProviderQuark)
	}

	if !quarkBYOAAuthExpired(http.StatusUnauthorized, Resp{}) {
		t.Fatal("HTTP 401 should be treated as expired Quark browser auth")
	}
	if !quarkBYOAAuthExpired(http.StatusOK, Resp{Code: http.StatusUnauthorized}) {
		t.Fatal("Quark code=401 should be treated as expired browser auth")
	}
	if !quarkBYOAAuthExpired(http.StatusOK, Resp{Message: "请登录后重试"}) {
		t.Fatal("explicit login-required message should be treated as expired browser auth")
	}
	if quarkBYOAAuthExpired(http.StatusForbidden, Resp{Message: "st invalid"}) {
		t.Fatal("share-token failure must not be treated as expired browser auth")
	}
	if quarkBYOAAuthExpired(http.StatusTooManyRequests, Resp{Message: "请求频繁"}) {
		t.Fatal("rate limiting must not be treated as expired browser auth")
	}
}
