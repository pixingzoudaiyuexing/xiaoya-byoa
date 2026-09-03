package byoa

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
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
			encoded, err := EncodeCredential(tt.provider, tt.credential)
			if err != nil {
				t.Fatalf("EncodeCredential() error = %v", err)
			}
			if strings.Contains(encoded, tt.credential) {
				t.Fatal("encrypted credential unexpectedly contains plaintext")
			}

			header := make(http.Header)
			header.Add("Cookie", (&http.Cookie{
				Name:  cookieName,
				Value: encoded,
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

	encodedA, err := EncodeCredential(ProviderQuark, "quark-cookie-A")
	if err != nil {
		t.Fatalf("EncodeCredential(A) error = %v", err)
	}
	encodedB, err := EncodeCredential(ProviderQuark, "quark-cookie-B")
	if err != nil {
		t.Fatalf("EncodeCredential(B) error = %v", err)
	}

	browserA := make(http.Header)
	browserA.Add("Cookie", (&http.Cookie{Name: cookieName, Value: encodedA}).String())

	browserB := make(http.Header)
	browserB.Add("Cookie", (&http.Cookie{Name: cookieName, Value: encodedB}).String())

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
	if encodedA == encodedB {
		t.Fatal("AES-GCM credentials should use distinct nonces")
	}
}

func TestEncryptedCredentialRejectsTampering(t *testing.T) {
	encoded, err := EncodeCredential(ProviderAliyun, "aliyun-secret")
	if err != nil {
		t.Fatalf("EncodeCredential() error = %v", err)
	}

	// 修改实际密文/认证标签字节，而不是只替换 base64url 最后一个字符。
	// Raw base64 的末字符可能包含未使用的低位，改变字符后仍可能解码成完全相同的字节，
	// 那不是密文被篡改，会造成概率性假失败。
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(encoded, credentialEncodingV1))
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}
	if len(payload) == 0 {
		t.Fatal("encrypted payload is empty")
	}
	payload[len(payload)-1] ^= 0x01
	tampered := credentialEncodingV1 + base64.RawURLEncoding.EncodeToString(payload)

	if _, err := DecodeCredential(ProviderAliyun, tampered); err == nil {
		t.Fatal("DecodeCredential() expected tamper error")
	}

	cookieName, _ := CookieName(ProviderAliyun)
	header := make(http.Header)
	header.Add("Cookie", (&http.Cookie{Name: cookieName, Value: tampered}).String())
	_, err = CredentialFromHeader(header, ProviderAliyun)
	if !IsAuthRequired(err) {
		t.Fatalf("tampered cookie should require re-auth, got %v", err)
	}
}

func TestEncryptedCredentialIsBoundToProvider(t *testing.T) {
	encoded, err := EncodeCredential(ProviderAliyun, "aliyun-secret")
	if err != nil {
		t.Fatalf("EncodeCredential() error = %v", err)
	}
	if _, err := DecodeCredential(ProviderQuark, encoded); err == nil {
		t.Fatal("Aliyun credential must not decrypt as Quark credential")
	}
}

func TestUnsupportedProvider(t *testing.T) {
	_, err := CredentialFromHeader(make(http.Header), Provider("unknown"))
	if !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("expected ErrUnsupportedProvider, got %v", err)
	}
}
