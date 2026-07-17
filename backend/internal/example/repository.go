package example

import (
	"context"
	"errors"
)

var (
	// ErrUserNotFound is returned by the repository when no user matches the id.
	ErrUserNotFound = errors.New("user not found")

	// ErrDuplicateKey is returned by the repository when a unique constraint is
	// violated (e.g. inserting a user whose id already exists).
	ErrDuplicateKey = errors.New("duplicate key violation")
)

// Repository is the storage contract for the example feature.
//
// Feature code depends on this interface, never on a concrete implementation.
// That split is what lets the Service be unit-tested against an in-memory fake
// (see service_test.go) while production wires in PostgresRepository.
type Repository interface {
	GetUserByID(ctx context.Context, id string) (*User, error)
	CreateUser(ctx context.Context, user *User) error
	CreateUserWithProfile(ctx context.Context, user *User, profile *Profile) error
	CreateUsers(ctx context.Context, users []*User) error
}
