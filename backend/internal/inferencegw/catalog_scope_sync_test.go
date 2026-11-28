package inferencegw

import (
	"context"
	"testing"
	"time"
)

type snapshotCatalogRepository struct {
	CatalogRepository
	snapshot *virtualCatalogSnapshot
}

func (r snapshotCatalogRepository) LoadSnapshot(context.Context) (*virtualCatalogSnapshot, error) {
	return r.snapshot, nil
}

func TestScopeRegistrationRefreshesStaleReplicaWithoutRepublishing(t *testing.T) {
	router := NewRoutingEngine(nil, nil, nil)
	router.LoadVirtualCatalog(newTestSnap(nil, nil))
	updated := make(chan struct{}, 1)
	router.SetCatalogUpdateHook(func([]CatalogScope) { updated <- struct{}{} })
	model := VirtualModel{Family: "colony-claude", Name: "claude-opus-5-5", Enabled: true}
	store := NewCatalogStore(snapshotCatalogRepository{snapshot: newTestSnap([]VirtualModel{model}, nil)}, router)

	if err := store.SyncForScopeRegistration(t.Context()); err != nil {
		t.Fatalf("sync catalog: %v", err)
	}
	var found bool
	for _, scope := range router.CatalogScopes() {
		found = found || scope.Scope == "inference:colony-claude:claude-opus-5-5:use"
	}
	if !found {
		t.Fatal("scope registration would prune a model on another replica")
	}
	select {
	case <-updated:
		t.Fatal("scope registration recursively triggered a catalog update")
	case <-time.After(20 * time.Millisecond):
	}
}
