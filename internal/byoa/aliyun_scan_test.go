package byoa

import (
	"encoding/base64"
	"testing"
)

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
