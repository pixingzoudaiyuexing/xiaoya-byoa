package byoa

import (
	"strings"
	"testing"
)

func TestAliyunQRStartTransportClassAllowlist(t *testing.T) {
	for _, class := range []string{"timeout", "dns", "eof", "tls", "reset", "refused", "other"} {
		err := newAliyunQRStartNetworkError(class)
		got, ok := AliyunQRStartTransportClass(err)
		if !ok {
			t.Fatalf("class %q was not exposed", class)
		}
		if got != class {
			t.Fatalf("class = %q, want %q", got, class)
		}
	}
}

func TestAliyunQRStartTransportClassRejectsArbitraryText(t *testing.T) {
	const secret = "secret-url-or-token"
	err := newAliyunQRStartNetworkError(secret)
	got, ok := AliyunQRStartTransportClass(err)
	if !ok {
		t.Fatal("expected sanitized transport class")
	}
	if got != "other" {
		t.Fatalf("class = %q, want other", got)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("arbitrary transport detail leaked into public error")
	}
}
