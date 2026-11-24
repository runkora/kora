package operator

import (
	"os"
	"testing"
)

// TestMain applies the install settings the chart always passes, as main does.
func TestMain(m *testing.M) {
	Apply(Settings{TrustDomain: "runkora.test"})
	os.Exit(m.Run())
}
