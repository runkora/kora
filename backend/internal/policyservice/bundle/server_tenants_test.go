package bundle

import (
	"context"
	"errors"
	"testing"
)

type unreadableRegistry struct{}

func (unreadableRegistry) TenantIDs() []string { return nil }
func (unreadableRegistry) Err() error          { return errors.New("parse tenant registry: bad yaml") }

func TestKnownTenantIDsFailsOnAnUnreadableRegistry(t *testing.T) {
	s := &Server{tenants: unreadableRegistry{}}
	if _, err := s.KnownTenantIDs(context.Background()); err == nil {
		t.Error("an unreadable registry with no last good contents was read as no tenants")
	}
}
