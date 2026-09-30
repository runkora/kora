package inferencegw

import "testing"

func TestIsBedrockFamily(t *testing.T) {
	t.Parallel()
	cases := []struct {
		provider ProviderName
		want     bool
	}{
		{ProviderBedrock, true},
		{"bedrock:us", true},
		{"bedrock:prod_east", true},
		{"bedrock:aus_2", true},
		{"bedrock:", false},         // bare colon — alias part is empty
		{"bedrock:1prod", true},     // resource names may start with a digit
		{"bedrock:prod-east", true}, // resource names may contain hyphens
		{"bedrock:-east", false},
		{"bedrock:Prod", false},
		{"vertexai", false},
		{"azure", false},
		{"", false},
		{"BEDROCK", false},
		{"bedrock-extra", false}, // hyphen, not colon
		{"notbedrock:alias", false},
	}
	for _, tc := range cases {
		t.Run(string(tc.provider), func(t *testing.T) {
			t.Parallel()
			if got := tc.provider.IsBedrockFamily(); got != tc.want {
				t.Errorf("IsBedrockFamily(%q) = %v, want %v", tc.provider, got, tc.want)
			}
		})
	}
}
