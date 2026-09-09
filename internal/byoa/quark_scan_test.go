package byoa

import (
	"net/http"
	"strings"
	"testing"
)

func TestCookieMapStringUsesLastValueAndStableOrder(t *testing.T) {
	cookies := cookieMap([]*http.Cookie{
		{Name: "z", Value: "1"},
		{Name: "a", Value: "old"},
		{Name: "a", Value: "new"},
	})
	got := cookieMapString(cookies)
	if got != "a=new; z=1" {
		t.Fatalf("cookieMapString() = %q", got)
	}
}

func TestQRDataURI(t *testing.T) {
	got, err := qrDataURI("https://example.com/scan?token=test")
	if err != nil {
		t.Fatalf("qrDataURI() error = %v", err)
	}
	if !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("qrDataURI() prefix = %q", got[:min(len(got), 32)])
	}
}
