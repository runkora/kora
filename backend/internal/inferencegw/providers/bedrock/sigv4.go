package bedrock

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// sigV4Transport is an http.RoundTripper that signs every outbound request
// with AWS SigV4 before handing off to the underlying transport.
// This allows the openai-go SDK to talk to Bedrock's OpenAI-compatible
// endpoint without any knowledge of AWS auth.
type sigV4Transport struct {
	signer *v4.Signer
	creds  aws.CredentialsProvider
	region string
	inner  http.RoundTripper
}

func newSigV4Transport(awsCfg aws.Config) http.RoundTripper {
	inner := http.DefaultTransport
	if awsCfg.HTTPClient != nil {
		if t, ok := awsCfg.HTTPClient.(*http.Client); ok && t.Transport != nil {
			inner = t.Transport
		}
	}
	return &sigV4Transport{
		signer: v4.NewSigner(),
		creds:  awsCfg.Credentials,
		region: awsCfg.Region,
		inner:  inner,
	}
}

func (t *sigV4Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Read body so we can hash it for SigV4, then restore it.
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
	}

	// Compute SHA-256 payload hash — required by Bedrock runtime.
	h := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(h[:])

	creds, err := t.creds.Retrieve(req.Context())
	if err != nil {
		return nil, err
	}

	// Clone so we don't mutate the caller's request.
	signed := req.Clone(req.Context())
	signed.Body = io.NopCloser(bytes.NewReader(body))
	signed.ContentLength = int64(len(body))

	if err := t.signer.SignHTTP(
		signed.Context(),
		creds,
		signed,
		payloadHash,
		"bedrock",
		t.region,
		time.Now(),
	); err != nil {
		return nil, err
	}

	return t.inner.RoundTrip(signed)
}
