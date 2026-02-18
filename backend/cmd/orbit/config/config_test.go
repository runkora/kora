package orbitConfig

import (
	"testing"
	"time"
)

func TestServerConfig_ShutdownTimeout(t *testing.T) {
	tests := []struct {
		name string
		secs int
		want time.Duration
	}{
		{"zero", 0, 0},
		{"five seconds (local default)", 5, 5 * time.Second},
		{"thirty seconds (production default)", 30, 30 * time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := OrbitConfig{Server: struct {
				Port                int    "yaml:\"port\""
				BasePath            string "yaml:\"basePath\""
				ShutdownTimeoutSecs int    "yaml:\"shutdownTimeoutSecs\""
			}{ShutdownTimeoutSecs: tc.secs}}
			if got := s.ShutdownTimeout(); got != tc.want {
				t.Errorf("ShutdownTimeout() = %v, want %v", got, tc.want)
			}
		})
	}
}
