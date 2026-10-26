package main

import (
	"io"
	"reflect"
	"testing"

	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/clear-route/orbit/internal/operator"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// recordingManager keeps what initControllers adds, so a test can see which
// reconcilers it built.
type recordingManager struct {
	ctrl.Manager
	added []manager.Runnable
}

func (m *recordingManager) Add(r manager.Runnable) error {
	m.added = append(m.added, r)
	return m.Manager.Add(r)
}

// reconcilers returns the reconciler behind each controller added, keyed by
// the controller's name. A controller holds both in exported fields (Name, Do)
// of a type controller-runtime keeps internal, so they are read by reflection.
func (m *recordingManager) reconcilers() map[string]reconcile.Reconciler {
	out := map[string]reconcile.Reconciler{}
	for _, r := range m.added {
		v := reflect.ValueOf(r)
		if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
			continue
		}
		name, do := v.Elem().FieldByName("Name"), v.Elem().FieldByName("Do")
		if !name.IsValid() || !do.IsValid() {
			continue
		}
		if rec, ok := do.Interface().(reconcile.Reconciler); ok {
			out[name.String()] = rec
		}
	}
	return out
}

func TestInitControllers_WiresSkillMarketplaceAggregation(t *testing.T) {
	skipNameValidation := true
	real, err := ctrl.NewManager(&rest.Config{Host: "http://127.0.0.1:1"}, ctrl.Options{
		Scheme:     operator.NewScheme(),
		Metrics:    metricsserver.Options{BindAddress: "0"},
		Controller: config.Controller{SkipNameValidation: &skipNameValidation},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr := &recordingManager{Manager: real}
	if err := initControllers(mgr, logging.NewLoggerWithOutput(io.Discard), nil); err != nil {
		t.Fatalf("initControllers: %v", err)
	}

	reconcilers := mgr.reconcilers()

	source, ok := reconcilers["skillmarketplacesource"].(*operator.SkillMarketplaceSourceReconciler)
	if !ok {
		t.Fatalf("controller skillmarketplacesource = %T, want *SkillMarketplaceSourceReconciler", reconcilers["skillmarketplacesource"])
	}
	if source.Aggregator == nil || source.Logger == nil {
		t.Errorf("SkillMarketplaceSourceReconciler has Aggregator=%v Logger=%v, want both set", source.Aggregator, source.Logger)
	}
	tenant, ok := reconcilers["orbittenant"].(*operator.TenantReconciler)
	if !ok {
		t.Fatalf("controller orbittenant = %T, want *TenantReconciler", reconcilers["orbittenant"])
	}
	if tenant.MarketplaceAggregator == nil {
		t.Error("TenantReconciler.MarketplaceAggregator is nil, so every tenant reconcile would panic")
	}
}
