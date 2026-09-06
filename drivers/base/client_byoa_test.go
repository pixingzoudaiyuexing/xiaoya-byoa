package base

import "testing"

func TestOutboundNetworkForAlipanAPI(t *testing.T) {
	tests := []struct {
		name    string
		network string
		address string
		want    string
	}{
		{
			name:    "alipan api forces ipv4",
			network: "tcp",
			address: "api.alipan.com:443",
			want:    "tcp4",
		},
		{
			name:    "alipan api ignores case",
			network: "tcp",
			address: "API.ALIPAN.COM:443",
			want:    "tcp4",
		},
		{
			name:    "other hosts keep default network",
			network: "tcp",
			address: "example.com:443",
			want:    "tcp",
		},
		{
			name:    "malformed address keeps default network",
			network: "tcp",
			address: "api.alipan.com",
			want:    "tcp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := outboundNetwork(tt.network, tt.address); got != tt.want {
				t.Fatalf("outboundNetwork(%q, %q) = %q, want %q", tt.network, tt.address, got, tt.want)
			}
		})
	}
}
