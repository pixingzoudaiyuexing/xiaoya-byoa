package net

import (
	"context"
	"crypto/tls"
	stdnet "net"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"testing"
	"time"
)

func TestDialTLSIPv4CandidatesFallsBackAfterBadAddress(t *testing.T) {
	server := httptest.NewTLSServer(nil)
	defer server.Close()

	_, port, err := stdnet.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split test server address: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dialTLSIPv4Candidates(
		ctx,
		"localhost",
		port,
		[]netip.Addr{
			netip.MustParseAddr("127.0.0.2"),
			netip.MustParseAddr("127.0.0.1"),
		},
		&tls.Config{InsecureSkipVerify: true},
		2*time.Second,
		true,
	)
	if err != nil {
		t.Fatalf("expected fallback TLS connection to succeed: %v", err)
	}
	defer conn.Close()
}

func TestIPv4TLSFallbackDisablesDefaultMLKEMCurves(t *testing.T) {
	transport := NewIPv4TLSFallbackTransport(nil, time.Second, true)
	got := transport.TLSClientConfig.CurvePreferences
	want := []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected curve preferences: got=%v want=%v", got, want)
	}
}

func TestLookupIPv4CandidatesRejectsIPv6Literal(t *testing.T) {
	_, err := lookupIPv4Candidates(context.Background(), "::1")
	if err == nil {
		t.Fatal("expected IPv6 literal to be rejected")
	}
}
