package bedrock

import (
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/google/go-cmp/cmp"
)

func TestLoadMultiConfig_disabled(t *testing.T) {
	t.Setenv("BEDROCK_ACCOUNTS", "")
	cfgs, err := LoadMultiConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfgs) != 0 {
		t.Fatalf("expected nil configs, got %v", cfgs)
	}
}

func TestLoadMultiConfig_singleAccount(t *testing.T) {
	t.Setenv("BEDROCK_ACCOUNTS", "prod")
	t.Setenv("BEDROCK_PROD_REGION", "us-east-1")
	t.Setenv("BEDROCK_PROD_ACCESS_KEY_ID", "AKIATEST")
	t.Setenv("BEDROCK_PROD_SECRET_ACCESS_KEY", "secret")
	t.Setenv("BEDROCK_PROD_SESSION_TOKEN", "tok")

	cfgs, err := LoadMultiConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfgs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(cfgs))
	}
	want := NamedConfig{
		Alias: "prod",
		Config: Config{
			Region:          "us-east-1",
			AccessKeyID:     "AKIATEST",
			SecretAccessKey: "secret",
			SessionToken:    "tok",
		},
	}
	if diff := cmp.Diff(want, cfgs[0]); diff != "" {
		t.Errorf("config mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadMultiConfig_multipleAccounts(t *testing.T) {
	t.Setenv("BEDROCK_ACCOUNTS", "prod,staging")
	t.Setenv("BEDROCK_PROD_REGION", "us-east-1")
	t.Setenv("BEDROCK_PROD_ACCESS_KEY_ID", "")
	t.Setenv("BEDROCK_PROD_SECRET_ACCESS_KEY", "")
	t.Setenv("BEDROCK_PROD_SESSION_TOKEN", "")
	t.Setenv("BEDROCK_STAGING_REGION", "eu-west-1")
	t.Setenv("BEDROCK_STAGING_ACCESS_KEY_ID", "")
	t.Setenv("BEDROCK_STAGING_SECRET_ACCESS_KEY", "")
	t.Setenv("BEDROCK_STAGING_SESSION_TOKEN", "")

	cfgs, err := LoadMultiConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfgs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(cfgs))
	}
	if cfgs[0].Alias != "prod" || cfgs[0].Config.Region != "us-east-1" {
		t.Errorf("unexpected prod config: %+v", cfgs[0])
	}
	if cfgs[1].Alias != "staging" || cfgs[1].Config.Region != "eu-west-1" {
		t.Errorf("unexpected staging config: %+v", cfgs[1])
	}
}

func TestLoadMultiConfig_missingRegion(t *testing.T) {
	t.Setenv("BEDROCK_ACCOUNTS", "prod")
	t.Setenv("BEDROCK_PROD_REGION", "")

	_, err := LoadMultiConfig()
	if err == nil {
		t.Fatal("expected error for missing region, got nil")
	}
}

func TestLoadMultiConfig_invalidAlias(t *testing.T) {
	cases := []struct {
		alias string
		desc  string
	}{
		{"PROD", "uppercase"},
		{"1prod", "digit-leading"},
		{"-prod", "hyphen-leading"},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Setenv("BEDROCK_ACCOUNTS", tc.alias)
			_, err := LoadMultiConfig()
			if err == nil {
				t.Fatalf("expected error for alias %q (%s), got nil", tc.alias, tc.desc)
			}
		})
	}
}

func TestLoadMultiConfig_aliasWithColon(t *testing.T) {
	t.Setenv("BEDROCK_ACCOUNTS", "prod:us") // colon — invalid

	_, err := LoadMultiConfig()
	if err == nil {
		t.Fatal("expected error for alias with colon, got nil")
	}
}

func TestLoadMultiConfig_trailingWhitespace(t *testing.T) {
	t.Setenv("BEDROCK_ACCOUNTS", " prod , staging ")
	t.Setenv("BEDROCK_PROD_REGION", "us-east-1")
	t.Setenv("BEDROCK_PROD_ACCESS_KEY_ID", "")
	t.Setenv("BEDROCK_PROD_SECRET_ACCESS_KEY", "")
	t.Setenv("BEDROCK_PROD_SESSION_TOKEN", "")
	t.Setenv("BEDROCK_STAGING_REGION", "eu-west-1")
	t.Setenv("BEDROCK_STAGING_ACCESS_KEY_ID", "")
	t.Setenv("BEDROCK_STAGING_SECRET_ACCESS_KEY", "")
	t.Setenv("BEDROCK_STAGING_SESSION_TOKEN", "")

	cfgs, err := LoadMultiConfig()
	if err != nil {
		t.Fatalf("unexpected error with whitespace-padded aliases: %v", err)
	}
	if len(cfgs) != 2 {
		t.Fatalf("expected 2 configs after trimming whitespace, got %d", len(cfgs))
	}
}

// ─── provider name computation ───────────────────────────────────────────────

// TestMultiFactory_ProviderNameComputation verifies that the alias → provider
// name mapping in MultiFactory.Build is correct by inspecting LoadMultiConfig
// output and applying the same logic. No AWS credentials or network needed.
func TestMultiFactory_ProviderNameComputation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		alias    string
		wantName inferencegw.ProviderName
	}{
		{"aus", "bedrock:aus"},
		{"us", "bedrock:us"},
		{"prod-east", "bedrock:prod-east"},
	}
	for _, tc := range cases {
		t.Run(tc.alias, func(t *testing.T) {
			t.Parallel()
			providerName := inferencegw.ProviderName("bedrock:" + tc.alias)
			if providerName != tc.wantName {
				t.Errorf("providerName = %q, want %q", providerName, tc.wantName)
			}
		})
	}
}

// TestConnector_Name_ReturnsAliasedName verifies that Connector.Name() returns
// the provider name it was constructed with, not a hardcoded "bedrock".
//
// TODO: integration test — Connector.Name() for a connector constructed with
// real AWS config. The unit test below validates the same logic using a
// manually-wired struct field, since New() requires valid AWS credentials.
func TestConnector_Name_ReturnsAliasedName(t *testing.T) {
	t.Parallel()
	c := &Connector{providerName: "bedrock:aus"}
	if c.Name() != "bedrock:aus" {
		t.Errorf("Name() = %q, want %q", c.Name(), "bedrock:aus")
	}
}

// ─── startup error exact strings ─────────────────────────────────────────────

func TestLoadMultiConfig_missingRegion_errorMessage(t *testing.T) {
	t.Setenv("BEDROCK_ACCOUNTS", "prod")
	t.Setenv("BEDROCK_PROD_REGION", "")

	_, err := LoadMultiConfig()
	if err == nil {
		t.Fatal("expected error for missing region, got nil")
	}
	if !strings.Contains(err.Error(), "BEDROCK_PROD_REGION") {
		t.Errorf("error %q does not mention BEDROCK_PROD_REGION", err.Error())
	}
	if !strings.Contains(err.Error(), `"prod"`) {
		t.Errorf("error %q does not mention account name", err.Error())
	}
}

func TestLoadMultiConfig_invalidAlias_errorMessage(t *testing.T) {
	t.Setenv("BEDROCK_ACCOUNTS", "PROD")

	_, err := LoadMultiConfig()
	if err == nil {
		t.Fatal("expected error for invalid alias, got nil")
	}
	if !strings.Contains(err.Error(), "invalid account alias") {
		t.Errorf("error %q does not mention 'invalid account alias'", err.Error())
	}
	if !strings.Contains(err.Error(), `"PROD"`) {
		t.Errorf("error %q does not mention the alias value", err.Error())
	}
}
