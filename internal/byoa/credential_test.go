package byoa

import (
	"errors"
	"net/http"
	"testing"
)

func TestCredentialFromHeader(t *testing.T) {
	tests := []struct {
		name       string
		provider   Provider
		credential string
	}{
		{
			name:       "aliyun token",
			provider:   ProviderAliyun,
			credential: "aliyun-refresh-token",
		},
		{
			name:       "quark cookie with separators",
			provider:   ProviderQuark,
			credential: "__pus=abc; __puus=def; kps=ghi; sign=jkl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cookieName, err := CookieName(tt.provider)
			if err != nil {
				t.Fatalf("CookieName() error = %v", err)
			}

			header := make(http.Header)
			header.Add("Cookie", (&http.Cookie{
				Name:  cookieName,
				Value: EncodeCredential(tt.credential),
			}).String())

			got, err := CredentialFromHeader(header, tt.provider)
			if err != nil {
				t.Fatalf("CredentialFromHeader() error = %v", err)
			}
			if got != tt.credential {
				t.Fatalf("CredentialFromHeader() = %q, want %q", got, tt.credential)
			}
		})
	}
}

func TestCredentialFromHeaderMissing(t *testing.T) {
	_, err := CredentialFromHeader(make(http.Header), ProviderAliyun)
	if err == nil {
		t.Fatal("CredentialFromHeader() expected error")
	}
	if !IsAuthRequired(err) {
		t.Fatalf("expected AuthRequiredError, got %T: %v", err, err)
	}

	var authErr *AuthRequiredError
	if !errors.As(err, &authErr) {
		t.Fatalf("errors.As() failed for %T", err)
	}
	if authErr.Provider != ProviderAliyun {
		t.Fatalf("provider = %q, want %q", authErr.Provider, ProviderAliyun)
	}
	if err.Error() != "BYOA_AUTH_REQUIRED:aliyun" {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestCredentialsAreIsolatedByBrowserHeader(t *testing.T) {
	cookieName, err := CookieName(ProviderQuark)
	if err != nil {
		t.Fatalf("CookieName() error = %v", err)
	}

	browserA := make(http.Header)
	browserA.Add("Cookie", (&http.Cookie{Name: cookieName, Value: EncodeCredential("quark-cookie-A")}).String())

	browserB := make(http.Header)
	browserB.Add("Cookie", (&http.Cookie{Name: cookieName, Value: EncodeCredential("quark-cookie-B")}).String())

	gotA, err := CredentialFromHeader(browserA, ProviderQuark)
	if err != nil {
		t.Fatalf("browser A error = %v", err)
	}
	gotB, err := CredentialFromHeader(browserB, ProviderQuark)
	if err != nil {
		t.Fatalf("browser B error = %v", err)
	}

	if gotA != "quark-cookie-A" || gotB != "quark-cookie-B" {
		t.Fatalf("browser credentials crossed: A=%q B=%q", gotA, gotB)
	}
}

func TestUnsupportedProvider(t *testing.T) {
	_, err := CredentialFromHeader(make(http.Header), Provider("unknown"))
	if !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("expected ErrUnsupportedProvider, got %v", err)
	}
}
