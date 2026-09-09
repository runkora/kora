package credentials_test

import (
	"testing"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/platform/connections"
)

// TestAdapterIDParity locks the two adapter identifier sets together. The
// platform metadata table declares its ids as plain strings so it carries no
// dependency on this package, which leaves nothing but this test to catch a
// typo. A mismatch is silent and far from its cause: the gateway still mints
// credentials, while the connector derives no auto-egress and offers no
// grantable actions, surfacing much later as a sandbox deny.
func TestAdapterIDParity(t *testing.T) {
	registrable := map[credentials.AdapterType]string{
		credentials.AdapterGitHubApp:        connections.AdapterGitHubApp,
		credentials.AdapterKubernetesBearer: connections.AdapterKubernetesBearer,
	}
	for typed, plain := range registrable {
		if string(typed) != plain {
			t.Errorf("adapter id mismatch: credentials %q vs connections %q", typed, plain)
		}
		if len(connections.Actions(plain)) == 0 {
			t.Errorf("adapter %q has no actions in the metadata table", plain)
		}
	}

	// Passthrough is deliberately absent from the metadata table: it is not
	// registrable, so it has no grantable vocabulary and no declared hosts.
	if len(connections.Actions(string(credentials.AdapterPassthrough))) != 0 {
		t.Error("passthrough must not appear in the connections metadata table")
	}
}
