package httpclient

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
)

// ErrInvalidCACert is returned when a CA bundle holds no PEM certificate.
var ErrInvalidCACert = errors.New("CA certificate is not valid PEM")

// NewTracingTransportWithCA is [NewTracingTransport] over a transport that also
// trusts caPEM, for an upstream signed by a private CA. An empty caPEM trusts
// the system roots only.
func NewTracingTransportWithCA(tracerName, caPEM string) (http.RoundTripper, error) {
	if caPEM == "" {
		return NewTracingTransport(tracerName, nil), nil
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
		return nil, ErrInvalidCACert
	}
	inner := http.DefaultTransport.(*http.Transport).Clone()
	inner.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return NewTracingTransport(tracerName, inner), nil
}
