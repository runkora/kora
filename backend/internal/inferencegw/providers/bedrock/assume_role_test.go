package bedrock

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

const assumeRoleResponse = `<AssumeRoleResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
<AssumeRoleResult><Credentials><AccessKeyId>ASIAASSUMED</AccessKeyId><SecretAccessKey>assumed-secret</SecretAccessKey>
<SessionToken>assumed-token</SessionToken><Expiration>2099-01-01T00:00:00Z</Expiration></Credentials>
<AssumedRoleUser><Arn>arn:aws:sts::123456789012:assumed-role/bedrock/orbit-inference-gateway</Arn><AssumedRoleId>AROA:orbit</AssumedRoleId></AssumedRoleUser>
</AssumeRoleResult><ResponseMetadata><RequestId>r1</RequestId></ResponseMetadata></AssumeRoleResponse>`

// TestAssumeRoleCredentials checks the STS call the gateway makes on top of its
// base credentials, against a stand-in STS endpoint.
func TestAssumeRoleCredentials(t *testing.T) {
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ = url.ParseQuery(string(body))
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, assumeRoleResponse)
	}))
	defer srv.Close()

	base := aws.Config{
		Region:       "us-east-1",
		Credentials:  credentials.NewStaticCredentialsProvider("AKIABASE", "base-secret", ""),
		BaseEndpoint: aws.String(srv.URL),
	}
	creds, err := assumeRoleCredentials(base, "arn:aws:iam::123456789012:role/bedrock", "ext-1", "").Retrieve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessKeyID != "ASIAASSUMED" || creds.SessionToken != "assumed-token" {
		t.Errorf("creds = %+v", creds)
	}
	want := map[string]string{
		"Action":          "AssumeRole",
		"RoleArn":         "arn:aws:iam::123456789012:role/bedrock",
		"ExternalId":      "ext-1",
		"RoleSessionName": defaultRoleSessionName,
	}
	for k, v := range want {
		if form.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, form.Get(k), v)
		}
	}
}

func TestAssumeRoleSessionNameFromResource(t *testing.T) {
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ = url.ParseQuery(string(body))
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, assumeRoleResponse)
	}))
	defer srv.Close()
	base := aws.Config{
		Region:       "us-east-1",
		Credentials:  credentials.NewStaticCredentialsProvider("AKIABASE", "base-secret", ""),
		BaseEndpoint: aws.String(srv.URL),
	}
	if _, err := assumeRoleCredentials(base, "arn:aws:iam::123456789012:role/bedrock", "", "team-a").Retrieve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if form.Get("RoleSessionName") != "team-a" || form.Has("ExternalId") {
		t.Errorf("form = %v", form)
	}
}
