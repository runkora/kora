package inferencegw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
)

// declaredCatalog writes declared virtual models and refreshes the routing cache.
type declaredCatalog interface {
	UpsertDeclaredVirtualModel(ctx context.Context, vm VirtualModel) (string, error)
	DeleteDeclaredVirtualModel(ctx context.Context, resourceName string) error
	ListDeclaredVirtualModels(ctx context.Context) ([]declarative.Declared, error)
}

// VirtualModelApplier projects the VirtualModel resources the operator sends
// into source = 'crd' catalog rows. VirtualModels are platform-wide, so the
// tenant the operator sends is ignored.
type VirtualModelApplier struct {
	catalog declaredCatalog
}

var _ declarative.Applier = (*VirtualModelApplier)(nil)

// NewVirtualModelApplier returns the Applier for declarative.KindVirtualModels.
func NewVirtualModelApplier(catalog declaredCatalog) *VirtualModelApplier {
	return &VirtualModelApplier{catalog: catalog}
}

// Apply validates the spec with the API's rules and upserts its row.
func (a *VirtualModelApplier) Apply(ctx context.Context, item declarative.Item) (declarative.Result, error) {
	var spec v1alpha1.VirtualModelSpec
	if err := json.Unmarshal(item.Spec, &spec); err != nil {
		return declarative.Result{Outcome: declarative.OutcomeInvalid, Message: "spec is not a VirtualModel spec: " + err.Error()}, nil
	}
	vm := declaredVirtualModel(item, spec)
	if err := ValidateVirtualModel(vm); err != nil {
		return declarative.Result{Outcome: declarative.OutcomeInvalid, Message: err.Error()}, nil
	}
	id, err := a.catalog.UpsertDeclaredVirtualModel(ctx, vm)
	switch {
	case errors.Is(err, ErrVirtualModelNameConflict):
		return declarative.Result{Outcome: declarative.OutcomeNameConflict, Message: vm.FullID() + " is held by another virtual model"}, nil
	case errors.Is(err, ErrVirtualModelRouteTaken):
		return declarative.Result{Outcome: declarative.OutcomeNameConflict, Message: err.Error()}, nil
	case err != nil:
		return declarative.Result{}, fmt.Errorf("apply VirtualModel %s: %w", item.Name, err)
	}
	return declarative.Result{Outcome: declarative.OutcomeProjected, ID: id, Ready: true}, nil
}

// Delete removes the named resource's row. A missing row is success.
func (a *VirtualModelApplier) Delete(ctx context.Context, _, name string) error {
	return a.catalog.DeleteDeclaredVirtualModel(ctx, name)
}

// List returns every declared virtual model.
func (a *VirtualModelApplier) List(ctx context.Context, _ string) ([]declarative.Declared, error) {
	return a.catalog.ListDeclaredVirtualModels(ctx)
}

// declaredVirtualModel maps a resource onto a catalog row. The IDs are used
// only when the row is new.
func declaredVirtualModel(item declarative.Item, spec v1alpha1.VirtualModelSpec) VirtualModel {
	vm := VirtualModel{
		ID:           uuid.New().String(),
		Family:       spec.Family,
		Name:         spec.Name,
		DisplayName:  spec.DisplayName,
		Description:  spec.Description,
		Strategy:     VirtualModelStrategy(spec.Strategy),
		Enabled:      spec.Enabled,
		Source:       SourceCRD,
		ResourceName: item.Name,
		ResourceUID:  item.UID,
		Routes:       make([]VirtualModelRoute, len(spec.Routes)),
	}
	for i, rt := range spec.Routes {
		vm.Routes[i] = VirtualModelRoute{
			ID:       uuid.New().String(),
			Provider: ProviderName(rt.Provider),
			ModelID:  rt.ModelID,
			Priority: int(rt.Priority),
			Weight:   int(rt.Weight),
			Enabled:  rt.Enabled,
		}
	}
	return vm
}
