package triggers

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Verification methods a generic source can use (docs/adr/2026-10-27-generic-webhook-source.md).
const (
	genericBearer           = "bearer"
	genericHMACSHA256       = "hmac_sha256"
	genericStandardWebhooks = "standard_webhooks"
)

// Signature encodings an hmac_sha256 source can use.
const (
	encodingHex    = "hex"
	encodingBase64 = "base64"
)

// genericEventTypeDefault is the event type of every delivery on a source with no event_type config.
const genericEventTypeDefault = "webhook"

// maxGenericPrefixBytes bounds the signature prefix an hmac_sha256 source strips.
const maxGenericPrefixBytes = 32

// standardWebhooksWindow is the replay window for Standard Webhooks timestamps, the same as Slack's.
const standardWebhooksWindow = 5 * time.Minute

// standardWebhooksSecretPrefix marks a Standard Webhooks secret. It is optional on a supplied secret.
const standardWebhooksSecretPrefix = "whsec_"

// headerNamePattern is the RFC 9110 token grammar an HTTP header name follows.
var headerNamePattern = regexp.MustCompile("^[!#$%&'*+\\-.^_`|~0-9A-Za-z]+$")

// genericConfig is the config a generic source stores in WebhookSource.Config.
type genericConfig struct {
	Verification *genericVerification `json:"verification"`
	EventType    *genericEventType    `json:"event_type"`
}

type genericVerification struct {
	Method   string `json:"method"`
	Header   string `json:"header"`
	Prefix   string `json:"prefix"`
	Encoding string `json:"encoding"`
}

type genericEventType struct {
	Header string `json:"header"`
	Path   string `json:"path"`
}

// genericAdapter is the provider for systems kora has no built-in adapter for.
// The registered value has no config and refuses every delivery.
type genericAdapter struct {
	cfg genericConfig
	// now is the clock for the Standard Webhooks window; nil means time.Now.
	now func() time.Time
}

// genericSuppliedAdapter is a generic adapter whose method accepts a secret the
// admin supplies. A bearer source is a plain genericAdapter.
type genericSuppliedAdapter struct {
	*genericAdapter
}

func (a *genericAdapter) Provider() string { return ProviderGeneric }

// ValidateConfig refuses a config the adapter cannot run with.
func (a *genericAdapter) ValidateConfig(config map[string]any) error {
	_, err := parseGenericConfig(config)
	return err
}

// ForSource returns the adapter for one source's config.
func (a *genericAdapter) ForSource(config map[string]any) (SourceAdapter, error) {
	cfg, err := parseGenericConfig(config)
	if err != nil {
		return nil, err
	}
	configured := &genericAdapter{cfg: cfg, now: a.now}
	if cfg.Verification.Method == genericBearer {
		return configured, nil
	}
	return &genericSuppliedAdapter{configured}, nil
}

// parseGenericConfig decodes a stored config strictly and fills its defaults.
func parseGenericConfig(config map[string]any) (genericConfig, error) {
	var cfg genericConfig
	raw, err := json.Marshal(config)
	if err != nil {
		return cfg, fmt.Errorf("%w: config: %w", ErrValidation, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("%w: config: %w", ErrValidation, err)
	}
	if err := checkGenericKeys(config); err != nil {
		return cfg, err
	}
	if err := validateGenericVerification(cfg.Verification); err != nil {
		return cfg, err
	}
	if err := validateGenericEventType(cfg.EventType, cfg.Verification.Header); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// genericKeys are the exact keys each config object takes. The JSON decoder
// matches keys without case, and the stored config must read as it runs.
var genericKeys = map[string][]string{
	"verification": {"method", "header", "prefix", "encoding"},
	"event_type":   {"header", "path"},
}

// checkGenericKeys refuses a key that is not spelled exactly as genericKeys lists it.
func checkGenericKeys(config map[string]any) error {
	for key, value := range config {
		allowed, ok := genericKeys[key]
		if !ok {
			return fmt.Errorf("%w: config.%s is not a known key", ErrValidation, key)
		}
		nested, _ := value.(map[string]any)
		for k := range nested {
			if !slices.Contains(allowed, k) {
				return fmt.Errorf("%w: config.%s.%s is not a known key", ErrValidation, key, k)
			}
		}
	}
	return nil
}

// reservedEventTypeHeaders carry credentials or signatures and must not become a stored event type.
var reservedEventTypeHeaders = []string{"Authorization", "Webhook-Id", "Webhook-Timestamp", "Webhook-Signature"}

func validateGenericVerification(v *genericVerification) error {
	if v == nil {
		return fmt.Errorf("%w: config.verification is required", ErrValidation)
	}
	switch v.Method {
	case genericBearer, genericStandardWebhooks:
		if v.Header != "" || v.Prefix != "" || v.Encoding != "" {
			return fmt.Errorf("%w: config.verification.header, prefix and encoding apply only to %s", ErrValidation, genericHMACSHA256)
		}
		return nil
	case genericHMACSHA256:
	default:
		return fmt.Errorf("%w: config.verification.method must be one of %s, %s or %s",
			ErrValidation, genericBearer, genericHMACSHA256, genericStandardWebhooks)
	}
	if !headerNamePattern.MatchString(v.Header) {
		return fmt.Errorf("%w: config.verification.header must be an HTTP header name", ErrValidation)
	}
	if len(v.Prefix) > maxGenericPrefixBytes {
		return fmt.Errorf("%w: config.verification.prefix must be at most %d bytes", ErrValidation, maxGenericPrefixBytes)
	}
	switch v.Encoding {
	case "":
		v.Encoding = encodingHex
	case encodingHex, encodingBase64:
	default:
		return fmt.Errorf("%w: config.verification.encoding must be %s or %s", ErrValidation, encodingHex, encodingBase64)
	}
	return nil
}

func validateGenericEventType(e *genericEventType, signatureHeader string) error {
	if e == nil {
		return nil
	}
	reserved := append(slices.Clone(reservedEventTypeHeaders), http.CanonicalHeaderKey(signatureHeader))
	switch {
	case e.Header != "" && slices.Contains(reserved, http.CanonicalHeaderKey(e.Header)):
		return fmt.Errorf("%w: config.event_type.header must not name a credential or signature header", ErrValidation)
	case (e.Header == "") == (e.Path == ""):
		return fmt.Errorf("%w: config.event_type needs exactly one of header or path", ErrValidation)
	case e.Header != "" && !headerNamePattern.MatchString(e.Header):
		return fmt.Errorf("%w: config.event_type.header must be an HTTP header name", ErrValidation)
	case e.Path != "" && !fieldKeyPattern.MatchString(e.Path):
		return fmt.Errorf("%w: config.event_type.path must be a dotted field path", ErrValidation)
	}
	return nil
}

// Verify checks a delivery with the source's method. Every comparison is constant time.
func (a *genericAdapter) Verify(secret string, headers http.Header, body []byte) error {
	if a.cfg.Verification == nil {
		return ErrSignatureInvalid
	}
	switch a.cfg.Verification.Method {
	case genericBearer:
		return verifyBearer(secret, headers)
	case genericHMACSHA256:
		return a.verifyHMAC(secret, headers, body)
	case genericStandardWebhooks:
		return a.verifyStandardWebhooks(secret, headers, body)
	}
	return ErrSignatureInvalid
}

func verifyBearer(secret string, headers http.Header) error {
	token, ok := strings.CutPrefix(headers.Get("Authorization"), "Bearer ")
	if !ok || !hmac.Equal([]byte(token), []byte(secret)) {
		return ErrSignatureInvalid
	}
	return nil
}

func (a *genericAdapter) verifyHMAC(secret string, headers http.Header, body []byte) error {
	v := a.cfg.Verification
	encoded, ok := strings.CutPrefix(headers.Get(v.Header), v.Prefix)
	if !ok || encoded == "" {
		return ErrSignatureInvalid
	}
	var got []byte
	var err error
	if v.Encoding == encodingBase64 {
		got, err = base64.StdEncoding.DecodeString(encoded)
	} else {
		got, err = hex.DecodeString(encoded)
	}
	if err != nil {
		return ErrSignatureInvalid
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return ErrSignatureInvalid
	}
	return nil
}

// verifyStandardWebhooks checks the Standard Webhooks v1 signature over "{id}.{timestamp}.{body}".
func (a *genericAdapter) verifyStandardWebhooks(secret string, headers http.Header, body []byte) error {
	key, err := standardWebhooksKey(secret)
	if err != nil {
		return ErrSignatureInvalid
	}
	id, ts := headers.Get("webhook-id"), headers.Get("webhook-timestamp")
	sec, err := strconv.ParseInt(ts, 10, 64)
	if id == "" || err != nil {
		return ErrSignatureInvalid
	}
	now := time.Now
	if a.now != nil {
		now = a.now
	}
	if age := now().Sub(time.Unix(sec, 0)); age > standardWebhooksWindow || age < -standardWebhooksWindow {
		return ErrSignatureInvalid
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "."))
	mac.Write(body)
	want := mac.Sum(nil)
	for entry := range strings.FieldsSeq(headers.Get("webhook-signature")) {
		encoded, ok := strings.CutPrefix(entry, "v1,")
		if !ok {
			continue
		}
		got, err := base64.StdEncoding.DecodeString(encoded)
		if err == nil && hmac.Equal(got, want) {
			return nil
		}
	}
	return ErrSignatureInvalid
}

// standardWebhooksKey decodes a Standard Webhooks secret into its HMAC key.
func standardWebhooksKey(secret string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, standardWebhooksSecretPrefix))
	if err != nil {
		return nil, fmt.Errorf("%w: signing_secret must be base64, with an optional %s prefix", ErrValidation, standardWebhooksSecretPrefix)
	}
	if len(key) == 0 {
		return nil, fmt.Errorf("%w: signing_secret holds no key", ErrValidation)
	}
	return key, nil
}

// DeliveryID is the hex SHA-256 of the body. An empty body has no id and is refused.
func (a *genericAdapter) DeliveryID(_ http.Header, body []byte) string {
	if len(body) == 0 {
		return ""
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// EventType reads the configured header or payload path. A source with no
// event_type config gives every delivery the type "webhook".
func (a *genericAdapter) EventType(headers http.Header, body []byte) string {
	e := a.cfg.EventType
	switch {
	case e == nil:
		return genericEventTypeDefault
	case e.Header != "":
		return cleanText(headers.Get(e.Header))
	default:
		return cleanText(resolvePath(decodeBody(body), e.Path))
	}
}

// Schema is nil: a generic source has no declared events.
func (a *genericAdapter) Schema() []byte { return nil }

// CheckSuppliedSecret refuses a supplied secret the method cannot use.
func (a *genericSuppliedAdapter) CheckSuppliedSecret(secret string) error {
	if a.cfg.Verification.Method == genericStandardWebhooks {
		_, err := standardWebhooksKey(secret)
		return err
	}
	return nil
}

// GenerateSecret returns a new secret in the form the method expects.
func (a *genericSuppliedAdapter) GenerateSecret() (string, error) {
	if a.cfg.Verification.Method != genericStandardWebhooks {
		return generateSecret()
	}
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return standardWebhooksSecretPrefix + base64.StdEncoding.EncodeToString(buf), nil
}
