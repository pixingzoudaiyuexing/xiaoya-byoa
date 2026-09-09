package bootstrap

import "testing"

func TestBYOARuntimeEnabled(t *testing.T) {
	tests := []struct {
		name string
		value string
		want bool
	}{
		{name: "unset", value: "", want: false},
		{name: "true", value: "true", want: true},
		{name: "one", value: "1", want: true},
		{name: "yes", value: "yes", want: true},
		{name: "on", value: "on", want: true},
		{name: "false", value: "false", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BYOA_XIAOYA_BOOTSTRAP", tt.value)
			if got := byoaRuntimeEnabled(); got != tt.want {
				t.Fatalf("byoaRuntimeEnabled() = %v, want %v for %q", got, tt.want, tt.value)
			}
		})
	}
}
