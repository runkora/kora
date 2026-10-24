package inferencegw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
)

type fakeDeclaredCatalog struct {
	rows      map[string]VirtualModel // by resource name
	upsertErr error
	writes    int
}

func (f *fakeDeclaredCatalog) UpsertDeclaredVirtualModel(_ context.Context, vm VirtualModel) (string, error) {
	if f.upsertErr != nil {
		return "", f.upsertErr
	}
	if row, ok := f.rows[vm.ResourceName]; ok && sameDeclared(row, vm) {
		return row.ID, nil
	} else if ok {
		vm.ID = row.ID
	}
	f.writes++
	f.rows[vm.ResourceName] = vm
	return vm.ID, nil
}

func (f *fakeDeclaredCatalog) DeleteDeclaredVirtualModel(_ context.Context, name string) error {
	delete(f.rows, name)
	return nil
}

func (f *fakeDeclaredCatalog) ListDeclaredVirtualModels(context.Context) ([]declarative.Declared, error) {
	var out []declarative.Declared
	for name, vm := range f.rows {
		out = append(out, declarative.Declared{Name: name, UID: vm.ResourceUID})
	}
	return out, nil
}

func virtualModelItem(t *testing.T, name string, spec v1alpha1.VirtualModelSpec) declarative.Item {
	t.Helper()
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	return declarative.Item{TenantID: declarative.PlatformTenant, Name: name, UID: "uid-" + name, Generation: 1, Spec: raw}
}

func sonnetSpec() v1alpha1.VirtualModelSpec {
	return v1alpha1.VirtualModelSpec{
		Family: "anthropic", Name: "sonnet", DisplayName: "Sonnet", Strategy: "priority", Enabled: true,
		Routes: []v1alpha1.VirtualModelRoute{{Provider: "bedrock:prod", ModelID: "m", Priority: 1, Enabled: true}},
	}
}

func TestVirtualModelApplierProjects(t *testing.T) {
	cat := &fakeDeclaredCatalog{rows: map[string]VirtualModel{}}
	a := NewVirtualModelApplier(cat)
	res, err := a.Apply(context.Background(), virtualModelItem(t, "claude-sonnet", sonnetSpec()))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != declarative.OutcomeProjected || !res.Ready || res.ID == "" {
		t.Fatalf("result = %+v, want Projected and ready with an id", res)
	}
	row := cat.rows["claude-sonnet"]
	if row.Source != declarative.SourceCRD || row.ResourceUID != "uid-claude-sonnet" || row.FullID() != "anthropic/sonnet" ||
		len(row.Routes) != 1 || row.Routes[0].Provider != "bedrock:prod" || row.Routes[0].ID == "" {
		t.Errorf("row = %+v", row)
	}

	again, err := a.Apply(context.Background(), virtualModelItem(t, "claude-sonnet", sonnetSpec()))
	if err != nil || again.ID != res.ID || again.Outcome != declarative.OutcomeProjected || cat.writes != 1 {
		t.Errorf("reapply = %+v err=%v writes=%d, want the same id and no write", again, err, cat.writes)
	}
}

func TestVirtualModelApplierRefusals(t *testing.T) {
	invalid := sonnetSpec()
	invalid.Strategy = "random"
	cases := []struct {
		name      string
		spec      json.RawMessage
		upsertErr error
		outcome   string
	}{
		{"invalid spec", mustJSON(t, invalid), nil, declarative.OutcomeInvalid},
		{"undecodable spec", json.RawMessage(`{"routes":"x"}`), nil, declarative.OutcomeInvalid},
		{"API row holds the key", mustJSON(t, sonnetSpec()), fmt.Errorf("upsert: %w", ErrVirtualModelNameConflict), declarative.OutcomeNameConflict},
		{"route taken", mustJSON(t, sonnetSpec()), fmt.Errorf("route x: %w", ErrVirtualModelRouteTaken), declarative.OutcomeNameConflict},
	}
	for _, tc := range cases {
		cat := &fakeDeclaredCatalog{rows: map[string]VirtualModel{}, upsertErr: tc.upsertErr}
		item := declarative.Item{TenantID: declarative.PlatformTenant, Name: "vm", UID: "uid", Spec: tc.spec}
		res, err := NewVirtualModelApplier(cat).Apply(context.Background(), item)
		if err != nil || res.Outcome != tc.outcome || res.Ready || res.Message == "" {
			t.Errorf("%s: result = %+v err = %v, want %s with a message", tc.name, res, err, tc.outcome)
		}
		if len(cat.rows) != 0 {
			t.Errorf("%s: a refused item wrote a row", tc.name)
		}
	}
}

func TestVirtualModelApplierStoreErrorFails(t *testing.T) {
	cat := &fakeDeclaredCatalog{rows: map[string]VirtualModel{}, upsertErr: errors.New("db down")}
	if _, err := NewVirtualModelApplier(cat).Apply(context.Background(), virtualModelItem(t, "vm", sonnetSpec())); err == nil {
		t.Error("a store failure was reported as an outcome")
	}
}

func TestVirtualModelApplierDeleteAndList(t *testing.T) {
	cat := &fakeDeclaredCatalog{rows: map[string]VirtualModel{}}
	a := NewVirtualModelApplier(cat)
	if _, err := a.Apply(context.Background(), virtualModelItem(t, "vm", sonnetSpec())); err != nil {
		t.Fatal(err)
	}
	listed, err := a.List(context.Background(), declarative.PlatformTenant)
	if err != nil || len(listed) != 1 || listed[0] != (declarative.Declared{Name: "vm", UID: "uid-vm"}) {
		t.Fatalf("list = %+v err = %v", listed, err)
	}
	for range 2 {
		if err := a.Delete(context.Background(), declarative.PlatformTenant, "vm"); err != nil {
			t.Fatalf("delete: %v", err)
		}
	}
	if listed, _ := a.List(context.Background(), declarative.PlatformTenant); len(listed) != 0 {
		t.Errorf("list after delete = %+v", listed)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
