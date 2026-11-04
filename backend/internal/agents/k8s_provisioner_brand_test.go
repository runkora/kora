package agents

import (
	"errors"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func TestNewK8sProvisioner_RejectsAnEmptyProductName(t *testing.T) {
	for _, name := range []string{"", "  "} {
		_, err := NewK8sProvisioner(false, logging.NewLogger(), K8sProvisionerConfig{ProductName: name}, nil)
		if !errors.Is(err, ErrProvisionerProductNameEmpty) {
			t.Errorf("ProductName %q: err = %v, want ErrProvisionerProductNameEmpty", name, err)
		}
	}
}

func TestNewRunService_RejectsAnEmptyProductName(t *testing.T) {
	_, err := NewRunService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, RunServiceConfig{}, logging.NewLogger())
	if !errors.Is(err, ErrRunServiceProductNameEmpty) {
		t.Errorf("err = %v, want ErrRunServiceProductNameEmpty", err)
	}
}
