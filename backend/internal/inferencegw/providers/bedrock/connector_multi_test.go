package bedrock

import (
	"testing"

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
	t.Setenv("BEDROCK_ACCOUNTS", "PROD") // uppercase — invalid

	_, err := LoadMultiConfig()
	if err == nil {
		t.Fatal("expected error for invalid alias, got nil")
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
