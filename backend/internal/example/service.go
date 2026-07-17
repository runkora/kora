package example

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

var (
	// ErrEmptyMessage is returned by Echo when the message is blank.
	ErrEmptyMessage = errors.New("message is required")

	// ErrMissingUserFields is returned by CreateUser when id or email is blank.
	ErrMissingUserFields = errors.New("id and email are required")

	// ErrUserExists is returned by CreateUser when the id is already taken. It is
	// the business-level translation of the repository's ErrDuplicateKey.
	ErrUserExists = errors.New("user already exists")
)

// Service holds the example feature's business logic. It depends only on the
// Repository interface and an injected logger — never on HTTP or a concrete DB —
// which keeps it unit-testable in isolation.
type Service struct {
	logger logging.Logger
	repo   Repository
}

// NewService constructs the example Service.
func NewService(logger logging.Logger, repo Repository) *Service {
	return &Service{logger: logger, repo: repo}
}

// Echo validates a message and returns it with its length.
func (s *Service) Echo(ctx context.Context, message string) (EchoResponse, error) {
	if message == "" {
		return EchoResponse{}, ErrEmptyMessage
	}
	s.logger.Info(ctx, "echo requested", logging.Field{Key: "length", Value: len(message)})
	return EchoResponse{Echo: message, Length: len(message)}, nil
}

// GetUser returns a user by id. The repository's ErrUserNotFound is wrapped so
// callers still match it with errors.Is.
func (s *Service) GetUser(ctx context.Context, id string) (*User, error) {
	user, err := s.repo.GetUserByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get user %q: %w", id, err)
	}
	return user, nil
}

// CreateUser validates the request, stamps the creation time, and persists the
// user. It returns ErrMissingUserFields on invalid input and ErrUserExists when
// the id is already taken.
func (s *Service) CreateUser(ctx context.Context, req CreateUserRequest) (*User, error) {
	if req.ID == "" || req.Email == "" {
		return nil, ErrMissingUserFields
	}

	user := &User{ID: req.ID, Email: req.Email, CreatedAt: time.Now().UTC()}
	if err := s.repo.CreateUser(ctx, user); err != nil {
		if errors.Is(err, ErrDuplicateKey) {
			return nil, fmt.Errorf("create user %q: %w", req.ID, ErrUserExists)
		}
		return nil, fmt.Errorf("create user %q: %w", req.ID, err)
	}

	s.logger.Info(ctx, "user created", logging.Field{Key: "user_id", Value: user.ID})
	return user, nil
}
