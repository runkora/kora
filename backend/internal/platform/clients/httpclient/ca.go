package httpclient

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
)

// ErrInvalidCACert is returned when a CA bundle holds no PEM certificate.
var ErrInvalidCACert = errors.New("CA certificate is not valid PEM")

// NewTransportWithCA returns a transport that trusts caPEM on top of the
// system roots, for an upstream signed by a private CA. An empty caPEM
// returns nil, which callers pass on to mean the default transport.
func NewTransportWithCA(caPEM string) (http.RoundTripper, error) {
	if caPEM == "" {
		return nil, nil
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
		return nil, ErrInvalidCACert
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default transport is %T", http.DefaultTransport)
	}
	t := base.Clone()
	t.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return t, nil
}
