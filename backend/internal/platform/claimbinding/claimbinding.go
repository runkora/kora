// Package claimbinding holds the tenant membership rules shared by the operator
// and the identity service: matching an ID token claim against a tenant's
// binding, and the email domain overlap rule.
// See docs/adr/2026-10-26-tenant-claim-binding-and-shared-email-domains.md.
package claimbinding

import (
	"fmt"
	"slices"
	"strings"
)

// Binding restricts tenant membership to people whose ID token claim equals one
// of Values or, for an array claim, contains one of them.
type Binding struct {
	Claim  string   `json:"claim"`
	Values []string `json:"values"`
}

// Matches reports whether claims satisfy the binding. A missing claim, an
// empty binding and a claim of an unsupported type never match.
func (b *Binding) Matches(claims map[string]any) bool {
	if b == nil || b.Claim == "" || len(b.Values) == 0 {
		return false
	}
	raw, ok := claims[b.Claim]
	if !ok {
		return false
	}
	if list, isList := raw.([]any); isList {
		return slices.ContainsFunc(list, b.hasValue)
	}
	return b.hasValue(raw)
}

// hasValue compares a scalar claim value as a string against Values.
func (b *Binding) hasValue(v any) bool {
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case bool, float64:
		s = fmt.Sprint(x)
	default:
		return false
	}
	return slices.Contains(b.Values, s)
}

// Tenant is the routing view of a tenant used by the overlap rule.
type Tenant struct {
	ID           string
	IssuerURL    string
	ClientID     string
	EmailDomains []string
	Binding      *Binding
}

// Shares reports whether two tenants may claim the same email domain: they use
// one issuer and client and each sets a claim binding.
func Shares(a, b Tenant) bool {
	return a.IssuerURL == b.IssuerURL && a.ClientID == b.ClientID &&
		a.Binding != nil && b.Binding != nil
}

// Overlap returns the domain that a and b both claim without meeting the
// shared-domain rule, or "" when they do not conflict.
func Overlap(a, b Tenant) string {
	if a.ID == b.ID || Shares(a, b) {
		return ""
	}
	for _, d := range a.EmailDomains {
		for _, e := range b.EmailDomains {
			if strings.EqualFold(d, e) {
				return strings.ToLower(d)
			}
		}
	}
	return ""
}
