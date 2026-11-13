package connections

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Header limits for credential headers. MCPServer spec.auth.headers and the
// apikey Connection config share them, so there is one source.
const (
	// MaxAPIKeyHeaders bounds how many headers one configuration injects.
	MaxAPIKeyHeaders = 8
	// MaxHeaderNameLength keeps a header name under the 253-character Secret
	// key limit once the aggregator adds its prefix.
	MaxHeaderNameLength = 64
	// MaxHeaderPrefixLength bounds the text placed before the secret.
	MaxHeaderPrefixLength = 256
)

// DefaultAPIKeyHeader and DefaultAPIKeyPrefix are used when an apikey config
// lists no headers.
const (
	DefaultAPIKeyHeader = "Authorization"
	DefaultAPIKeyPrefix = "Bearer "
)

// ErrInvalidAPIKeyConfig is returned by ParseAPIKeyConfig for a malformed
// config. The credentials adapter wraps it with credentials.ErrInvalidConfig.
var ErrInvalidAPIKeyConfig = errors.New("invalid apikey connection config")

// reservedHeaders are set by HTTP, the proxy, tracing or the platform. A
// credential header must not replace them.
var reservedHeaders = map[string]bool{
	"host": true, "content-length": true, "content-type": true, "accept": true,
	"transfer-encoding": true, "connection": true, "keep-alive": true,
	"proxy-connection": true, "proxy-authenticate": true, "proxy-authorization": true,
	"te": true, "trailer": true, "upgrade": true,
	"mcp-session-id": true, "mcp-protocol-version": true, "last-event-id": true,
	"traceparent": true, "tracestate": true,
	"x-kora-delegation-token": true, "x-kora-run-id": true,
}

// reservedHeaderPrefixes cover proxy and Envoy control headers.
var reservedHeaderPrefixes = []string{"x-forwarded-", "x-envoy-"}

// IsReservedHeader reports whether a credential header may not use name. The
// match is case-insensitive.
func IsReservedHeader(name string) bool {
	lower := strings.ToLower(name)
	if reservedHeaders[lower] {
		return true
	}
	for _, p := range reservedHeaderPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

// IsHeaderName reports whether name uses only letters, digits and hyphens.
// That subset of RFC 7230 tokens is also valid in a Secret data key.
func IsHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// IsHeaderValue rejects control characters other than tab, as net/http does.
func IsHeaderValue(v []byte) bool {
	for _, c := range v {
		if (c < 0x20 && c != '\t') || c == 0x7f {
			return false
		}
	}
	return true
}

// APIKeyHeader is one header an apikey connection injects.
type APIKeyHeader struct {
	Name string `json:"name"`
	// Prefix is prepended to the secret. An omitted prefix is empty: a raw
	// value, as for x-api-key.
	Prefix string `json:"prefix"`
}

// APIKeyConfig is the adapter configuration of an apikey Connection. Every
// key is optional.
type APIKeyConfig struct {
	// Headers lists the headers to inject, 1 to MaxAPIKeyHeaders. After parsing
	// it is never empty: the default is Authorization: Bearer <secret>.
	Headers []APIKeyHeader `json:"headers"`
	// PlaceholderEnv names a sandbox env var set to the placeholder.
	PlaceholderEnv string `json:"placeholderEnv"`
}

// ParseAPIKeyConfig decodes and validates an apikey Connection config once.
// An empty or null config yields the default header. Unknown keys are
// rejected to catch typos. Errors wrap ErrInvalidAPIKeyConfig.
func ParseAPIKeyConfig(raw json.RawMessage) (APIKeyConfig, error) {
	defaults := []APIKeyHeader{{Name: DefaultAPIKeyHeader, Prefix: DefaultAPIKeyPrefix}}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return APIKeyConfig{Headers: defaults}, nil
	}
	var cfg APIKeyConfig
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return APIKeyConfig{}, fmt.Errorf("%w: %w", ErrInvalidAPIKeyConfig, err)
	}
	if dec.More() {
		return APIKeyConfig{}, fmt.Errorf("%w: trailing data", ErrInvalidAPIKeyConfig)
	}
	if cfg.Headers == nil {
		cfg.Headers = defaults
		return cfg, nil
	}
	if n := len(cfg.Headers); n == 0 || n > MaxAPIKeyHeaders {
		return APIKeyConfig{}, fmt.Errorf("%w: headers must list 1 to %d entries", ErrInvalidAPIKeyConfig, MaxAPIKeyHeaders)
	}
	seen := make(map[string]bool, len(cfg.Headers))
	for _, h := range cfg.Headers {
		lower := strings.ToLower(h.Name)
		switch {
		case len(h.Name) > MaxHeaderNameLength:
			return APIKeyConfig{}, fmt.Errorf("%w: a header name is over %d characters", ErrInvalidAPIKeyConfig, MaxHeaderNameLength)
		case !IsHeaderName(h.Name):
			return APIKeyConfig{}, fmt.Errorf("%w: %q must use only letters, digits and hyphens", ErrInvalidAPIKeyConfig, h.Name)
		case IsReservedHeader(h.Name):
			return APIKeyConfig{}, fmt.Errorf("%w: header %q is reserved", ErrInvalidAPIKeyConfig, h.Name)
		case seen[lower]:
			return APIKeyConfig{}, fmt.Errorf("%w: header %q is listed twice", ErrInvalidAPIKeyConfig, h.Name)
		case len(h.Prefix) > MaxHeaderPrefixLength:
			return APIKeyConfig{}, fmt.Errorf("%w: the prefix of %q is over %d characters", ErrInvalidAPIKeyConfig, h.Name, MaxHeaderPrefixLength)
		case !IsHeaderValue([]byte(h.Prefix)):
			return APIKeyConfig{}, fmt.Errorf("%w: the prefix of %q contains a control character", ErrInvalidAPIKeyConfig, h.Name)
		}
		seen[lower] = true
	}
	return cfg, nil
}
