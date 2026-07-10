package example

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// silentLogger returns a logger that discards output, so tests stay quiet.
func silentLogger() logging.Logger { return logging.NewLoggerWithOutput(io.Discard) }

// fakeRepo is an in-memory Repository for unit-testing the Service without a
// database. This is the pattern a new feature copies to test its own service.
type fakeRepo struct {
	users     map[string]*User
	createErr error // when set, CreateUser returns it (e.g. ErrDuplicateKey)
}

func newFakeRepo() *fakeRepo { return &fakeRepo{users: map[string]*User{}} }

func (f *fakeRepo) GetUserByID(_ context.Context, id string) (*User, error) {
	u, ok := f.users[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u, nil
}

func (f *fakeRepo) CreateUser(_ context.Context, u *User) error {
	if f.createErr != nil {
		return f.createErr
	}
	if _, exists := f.users[u.ID]; exists {
		return ErrDuplicateKey
	}
	f.users[u.ID] = u
	return nil
}

func (f *fakeRepo) CreateUserWithProfile(_ context.Context, _ *User, _ *Profile) error { return nil }
func (f *fakeRepo) CreateUsers(_ context.Context, _ []*User) error                     { return nil }

func TestServiceEcho(t *testing.T) {
	tests := []struct {
		name    string
		message string
		wantLen int
		wantErr error
	}{
		{name: "echoes message", message: "hi", wantLen: 2},
		{name: "empty is rejected", message: "", wantErr: ErrEmptyMessage},
	}

	svc := NewService(silentLogger(), newFakeRepo())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := svc.Echo(context.Background(), tt.message)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil {
				if resp.Echo != tt.message || resp.Length != tt.wantLen {
					t.Errorf("resp = %+v, want echo=%q len=%d", resp, tt.message, tt.wantLen)
				}
			}
		})
	}
}

func TestServiceCreateUser(t *testing.T) {
	tests := []struct {
		name      string
		req       CreateUserRequest
		createErr error
		wantErr   error
	}{
		{name: "valid", req: CreateUserRequest{ID: "u1", Email: "u1@example.com"}},
		{name: "missing id", req: CreateUserRequest{Email: "u@example.com"}, wantErr: ErrMissingUserFields},
		{name: "missing email", req: CreateUserRequest{ID: "u2"}, wantErr: ErrMissingUserFields},
		{name: "duplicate maps to ErrUserExists", req: CreateUserRequest{ID: "u3", Email: "u3@example.com"}, createErr: ErrDuplicateKey, wantErr: ErrUserExists},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepo()
			repo.createErr = tt.createErr
			svc := NewService(silentLogger(), repo)

			user, err := svc.CreateUser(context.Background(), tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if user.ID != tt.req.ID || user.Email != tt.req.Email {
				t.Errorf("user = %+v, want id=%q email=%q", user, tt.req.ID, tt.req.Email)
			}
			if user.CreatedAt.IsZero() {
				t.Error("CreatedAt was not stamped")
			}
		})
	}
}

func TestServiceGetUser(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(silentLogger(), repo)
	if _, err := svc.CreateUser(context.Background(), CreateUserRequest{ID: "u1", Email: "u1@example.com"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if got, err := svc.GetUser(context.Background(), "u1"); err != nil || got.ID != "u1" {
		t.Fatalf("GetUser(u1) = %+v, %v", got, err)
	}
	if _, err := svc.GetUser(context.Background(), "missing"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("GetUser(missing) err = %v, want ErrUserNotFound", err)
	}
}
