package agents

import (
	"errors"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func TestNewK8sProvisioner_RejectsAnEmptyProductName(t *testing.T) {
	for _, name := range []string{"", "  "} {
		_, err := NewK8sProvisioner(false, logging.NewLogger(), K8sProvisionerConfig{ProductName: name}, nil)
		if !errors.Is(err, ErrProvisionerProductNameEmpty) {
			t.Errorf("ProductName %q: err = %v, want ErrProvisionerProductNameEmpty", name, err)
		}
	}
}
