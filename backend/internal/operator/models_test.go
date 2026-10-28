package operator

import "testing"

// A shared backing array would let a reconciler editing its copy of a cached
// tenant change the informer's object underneath every other reader.
func TestOrbitTenantDeepCopy_CopiesLists(t *testing.T) {
	in := &OrbitTenant{Spec: OrbitTenantSpec{
		AllowedMarketplaceRepos: []string{"acme-corp/internal-skills"},
		OIDC:                    TenantOIDCSpec{EmailDomains: []string{"acme.com"}},
	}}
	out := in.DeepCopy()
	out.Spec.AllowedMarketplaceRepos[0] = "globex/skills"
	out.Spec.OIDC.EmailDomains[0] = "globex.com"

	if got := in.Spec.AllowedMarketplaceRepos[0]; got != "acme-corp/internal-skills" {
		t.Errorf("AllowedMarketplaceRepos shares its array with the copy: original is now %q", got)
	}
	if got := in.Spec.OIDC.EmailDomains[0]; got != "acme.com" {
		t.Errorf("EmailDomains shares its array with the copy: original is now %q", got)
	}
}
