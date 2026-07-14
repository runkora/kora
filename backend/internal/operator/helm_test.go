package operator

import "testing"

// TestShouldSkipUpgrade guards pinned decision 20: the skip-if-at-version
// optimisation must key on the requested chartVersion parameter, never the
// loaded chart's own Chart.yaml Metadata.Version (always non-empty) — a
// local (baked) chart is always requested with chartVersion == "" and must
// never skip, or edited chart templates would silently stop being applied.
func TestShouldSkipUpgrade(t *testing.T) {
	tests := []struct {
		name                 string
		deployed             bool
		requestedVersion     string
		deployedChartVersion string
		want                 bool
	}{
		{
			name:                 "deployed at exactly the requested pinned version skips",
			deployed:             true,
			requestedVersion:     "1.29.1",
			deployedChartVersion: "1.29.1",
			want:                 true,
		},
		{
			name:                 "deployed at a different pinned version upgrades",
			deployed:             true,
			requestedVersion:     "1.29.1",
			deployedChartVersion: "1.28.0",
			want:                 false,
		},
		{
			name:                 "empty requested version (local/baked chart) never skips",
			deployed:             true,
			requestedVersion:     "",
			deployedChartVersion: "0.1.0",
			want:                 false,
		},
		{
			name:                 "not deployed never skips",
			deployed:             false,
			requestedVersion:     "1.29.1",
			deployedChartVersion: "1.29.1",
			want:                 false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldSkipUpgrade(tt.deployed, tt.requestedVersion, tt.deployedChartVersion)
			if got != tt.want {
				t.Errorf("shouldSkipUpgrade(%v, %q, %q) = %v, want %v",
					tt.deployed, tt.requestedVersion, tt.deployedChartVersion, got, tt.want)
			}
		})
	}
}
