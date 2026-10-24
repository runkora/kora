package declarative

import (
	"errors"
	"io"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"

	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

func TestManagerOptions(t *testing.T) {
	o, err := managerOptions(ManagerOptions{
		Service:         "kora-agents",
		SystemNamespace: "kora-system",
		Namespaces:      []string{"kora-system"},
		Logger:          logging.NewLoggerWithOutput(io.Discard),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !o.LeaderElection || o.LeaderElectionID != "kora-agents-declarative" || o.LeaderElectionNamespace != "kora-system" {
		t.Errorf("leader election = %v %q %q", o.LeaderElection, o.LeaderElectionID, o.LeaderElectionNamespace)
	}
	if o.Metrics.BindAddress != "0" {
		t.Errorf("metrics bind address = %q, want 0", o.Metrics.BindAddress)
	}
	if o.Cache.SyncPeriod == nil || *o.Cache.SyncPeriod != ResyncPeriod {
		t.Errorf("sync period = %v", o.Cache.SyncPeriod)
	}
	if _, ok := o.Cache.DefaultNamespaces["kora-system"]; !ok || len(o.Cache.DefaultNamespaces) != 1 {
		t.Errorf("cache namespaces = %v", o.Cache.DefaultNamespaces)
	}
	if len(o.Client.Cache.DisableFor) != 1 {
		t.Fatalf("uncached kinds = %v", o.Client.Cache.DisableFor)
	}
	if _, ok := o.Client.Cache.DisableFor[0].(*corev1.Secret); !ok {
		t.Errorf("uncached kind = %T, want Secret", o.Client.Cache.DisableFor[0])
	}
	for _, obj := range []runtime.Object{&v1alpha1.Agent{}, &v1alpha1.TriggerList{}, &corev1.Secret{}} {
		if _, _, err := o.Scheme.ObjectKinds(obj); err != nil {
			t.Errorf("scheme: %v", err)
		}
	}
}

func TestManagerOptionsRequired(t *testing.T) {
	if _, err := NewManager(&rest.Config{Host: "https://127.0.0.1:1"}, ManagerOptions{Service: "x"}); !errors.Is(err, ErrManagerOptions) {
		t.Errorf("err = %v, want ErrManagerOptions", err)
	}
}

func TestNewManager(t *testing.T) {
	mgr, err := NewManager(&rest.Config{Host: "https://127.0.0.1:1"}, ManagerOptions{
		Service: "kora-triggers", SystemNamespace: "kora-system", Logger: logging.NewLoggerWithOutput(io.Discard),
	})
	if err != nil {
		t.Fatal(err)
	}
	if mgr.GetAPIReader() == nil {
		t.Error("no uncached reader")
	}
}
