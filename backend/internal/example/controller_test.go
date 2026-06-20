package example

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// ── test helpers ──────────────────────────────────────────────────────────────

// mockRepository is a test double for Repository that allows configuring
// return values and tracking calls.
type mockRepository struct {
	getUserByIDFunc            func(ctx context.Context, id string) (*User, error)
	createUserFunc             func(ctx context.Context, user *User) error
	createUserWithProfileFunc  func(ctx context.Context, user *User, profile *Profile) error
	createUsersFunc            func(ctx context.Context, users []*User) error
}

func (m *mockRepository) GetUserByID(ctx context.Context, id string) (*User, error) {
	if m.getUserByIDFunc != nil {
		return m.getUserByIDFunc(ctx, id)
	}
	return nil, ErrUserNotFound
}

func (m *mockRepository) CreateUser(ctx context.Context, user *User) error {
	if m.createUserFunc != nil {
		return m.createUserFunc(ctx, user)
	}
	return nil
}

func (m *mockRepository) CreateUserWithProfile(ctx context.Context, user *User, profile *Profile) error {
	if m.createUserWithProfileFunc != nil {
		return m.createUserWithProfileFunc(ctx, user, profile)
	}
	return nil
}

func (m *mockRepository) CreateUsers(ctx context.Context, users []*User) error {
	if m.createUsersFunc != nil {
		return m.createUsersFunc(ctx, users)
	}
	return nil
}

// discardLogger returns a logger that discards all output.
func discardLogger() logging.Logger {
	return logging.NewLoggerWithOutput(bytes.NewBuffer(nil))
}

// ── handleHealth ──────────────────────────────────────────────────────────────

func TestController_HandleHealth_ReturnsHealthy(t *testing.T) {
	ctrl := NewController(discardLogger(), &mockRepository{})

	req := httptest.NewRequest(http.MethodGet, "/example/health", nil)
	w := httptest.NewRecorder()

	ctrl.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["status"] != "healthy" {
		t.Errorf("status: got %q, want %q", resp["status"], "healthy")
	}
	if resp["service"] != "orbit-example" {
		t.Errorf("service: got %q, want %q", resp["service"], "orbit-example")
	}
}

// ── handleHello ───────────────────────────────────────────────────────────────

func TestController_HandleHello_WithValidName(t *testing.T) {
	ctrl := NewController(discardLogger(), &mockRepository{})

	req := httptest.NewRequest(http.MethodGet, "/example/hello/alice", nil)
	req.SetPathValue("name", "alice")
	w := httptest.NewRecorder()

	ctrl.handleHello(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	want := "Hello, alice!"
	if resp["message"] != want {
		t.Errorf("message: got %q, want %q", resp["message"], want)
	}
}

func TestController_HandleHello_WithSpecialCharacters(t *testing.T) {
	ctrl := NewController(discardLogger(), &mockRepository{})

	testCases := []struct {
		name     string
		want     string
	}{
		{"alice@example.com", "Hello, alice@example.com!"},
		{"user-123", "Hello, user-123!"},
		{"user_456", "Hello, user_456!"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/example/hello/"+tc.name, nil)
			req.SetPathValue("name", tc.name)
			w := httptest.NewRecorder()

			ctrl.handleHello(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
			}

			var resp map[string]string
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			if resp["message"] != tc.want {
				t.Errorf("message: got %q, want %q", resp["message"], tc.want)
			}
		})
	}
}

// ── handleEcho ────────────────────────────────────────────────────────────────

func TestController_HandleEcho_ValidMessage(t *testing.T) {
	ctrl := NewController(discardLogger(), &mockRepository{})

	body := []byte(`{"message":"hello world"}`)
	req := httptest.NewRequest(http.MethodPost, "/example/echo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctrl.handleEcho(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["echo"] != "hello world" {
		t.Errorf("echo: got %q, want %q", resp["echo"], "hello world")
	}
	if resp["length"] != float64(11) { // JSON numbers are float64
		t.Errorf("length: got %v, want %v", resp["length"], 11)
	}
}

func TestController_HandleEcho_EmptyMessage(t *testing.T) {
	ctrl := NewController(discardLogger(), &mockRepository{})

	body := []byte(`{"message":""}`)
	req := httptest.NewRequest(http.MethodPost, "/example/echo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctrl.handleEcho(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["message"] != "message field is required" {
		t.Errorf("message: got %q, want %q", resp["message"], "message field is required")
	}
}

func TestController_HandleEcho_MissingField(t *testing.T) {
	ctrl := NewController(discardLogger(), &mockRepository{})

	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/example/echo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctrl.handleEcho(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestController_HandleEcho_InvalidJSON(t *testing.T) {
	ctrl := NewController(discardLogger(), &mockRepository{})

	body := []byte(`{invalid json}`)
	req := httptest.NewRequest(http.MethodPost, "/example/echo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctrl.handleEcho(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ── handleGetUser ─────────────────────────────────────────────────────────────

func TestController_HandleGetUser_UserExists(t *testing.T) {
	mockRepo := &mockRepository{
		getUserByIDFunc: func(ctx context.Context, id string) (*User, error) {
			if id == "user-123" {
				return &User{
					ID:        "user-123",
					Email:     "alice@example.com",
					CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				}, nil
			}
			return nil, ErrUserNotFound
		},
	}
	ctrl := NewController(discardLogger(), mockRepo)

	req := httptest.NewRequest(http.MethodGet, "/example/users/user-123", nil)
	req.SetPathValue("id", "user-123")
	w := httptest.NewRecorder()

	ctrl.handleGetUser(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["id"] != "user-123" {
		t.Errorf("id: got %q, want %q", resp["id"], "user-123")
	}
	if resp["email"] != "alice@example.com" {
		t.Errorf("email: got %q, want %q", resp["email"], "alice@example.com")
	}
}

func TestController_HandleGetUser_UserNotFound(t *testing.T) {
	mockRepo := &mockRepository{
		getUserByIDFunc: func(ctx context.Context, id string) (*User, error) {
			return nil, ErrUserNotFound
		},
	}
	ctrl := NewController(discardLogger(), mockRepo)

	req := httptest.NewRequest(http.MethodGet, "/example/users/nonexistent", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()

	ctrl.handleGetUser(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["message"] != "user not found" {
		t.Errorf("message: got %q, want %q", resp["message"], "user not found")
	}
}

func TestController_HandleGetUser_DatabaseError(t *testing.T) {
	mockRepo := &mockRepository{
		getUserByIDFunc: func(ctx context.Context, id string) (*User, error) {
			return nil, errors.New("database connection failed")
		},
	}
	ctrl := NewController(discardLogger(), mockRepo)

	req := httptest.NewRequest(http.MethodGet, "/example/users/user-123", nil)
	req.SetPathValue("id", "user-123")
	w := httptest.NewRecorder()

	ctrl.handleGetUser(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusInternalServerError)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["message"] != "internal server error" {
		t.Errorf("message: got %q, want %q", resp["message"], "internal server error")
	}
}

// ── handleCreateUser ──────────────────────────────────────────────────────────

func TestController_HandleCreateUser_Success(t *testing.T) {
	var capturedUser *User
	mockRepo := &mockRepository{
		createUserFunc: func(ctx context.Context, user *User) error {
			capturedUser = user
			return nil
		},
	}
	ctrl := NewController(discardLogger(), mockRepo)

	body := []byte(`{"id":"user-456","email":"bob@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/example/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctrl.handleCreateUser(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusCreated)
	}

	if capturedUser == nil {
		t.Fatal("expected CreateUser to be called")
	}
	if capturedUser.ID != "user-456" {
		t.Errorf("captured user ID: got %q, want %q", capturedUser.ID, "user-456")
	}
	if capturedUser.Email != "bob@example.com" {
		t.Errorf("captured user email: got %q, want %q", capturedUser.Email, "bob@example.com")
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["id"] != "user-456" {
		t.Errorf("response id: got %q, want %q", resp["id"], "user-456")
	}
	if resp["email"] != "bob@example.com" {
		t.Errorf("response email: got %q, want %q", resp["email"], "bob@example.com")
	}
}

func TestController_HandleCreateUser_MissingID(t *testing.T) {
	ctrl := NewController(discardLogger(), &mockRepository{})

	body := []byte(`{"email":"bob@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/example/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctrl.handleCreateUser(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["message"] != "id and email fields are required" {
		t.Errorf("message: got %q, want %q", resp["message"], "id and email fields are required")
	}
}

func TestController_HandleCreateUser_MissingEmail(t *testing.T) {
	ctrl := NewController(discardLogger(), &mockRepository{})

	body := []byte(`{"id":"user-789"}`)
	req := httptest.NewRequest(http.MethodPost, "/example/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctrl.handleCreateUser(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestController_HandleCreateUser_MissingBothFields(t *testing.T) {
	ctrl := NewController(discardLogger(), &mockRepository{})

	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/example/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctrl.handleCreateUser(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Should report both missing fields.
	missingFields, ok := resp["details"].(map[string]any)
	if !ok {
		t.Fatal("expected details field in response")
	}
	fields, ok := missingFields["missing_fields"].([]any)
	if !ok || len(fields) != 2 {
		t.Errorf("expected 2 missing fields, got %v", fields)
	}
}

func TestController_HandleCreateUser_DuplicateKey(t *testing.T) {
	mockRepo := &mockRepository{
		createUserFunc: func(ctx context.Context, user *User) error {
			return ErrDuplicateKey
		},
	}
	ctrl := NewController(discardLogger(), mockRepo)

	body := []byte(`{"id":"user-duplicate","email":"duplicate@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/example/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctrl.handleCreateUser(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusConflict)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["message"] != "user already exists" {
		t.Errorf("message: got %q, want %q", resp["message"], "user already exists")
	}
}

func TestController_HandleCreateUser_DatabaseError(t *testing.T) {
	mockRepo := &mockRepository{
		createUserFunc: func(ctx context.Context, user *User) error {
			return errors.New("database connection failed")
		},
	}
	ctrl := NewController(discardLogger(), mockRepo)

	body := []byte(`{"id":"user-error","email":"error@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/example/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctrl.handleCreateUser(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusInternalServerError)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["message"] != "failed to create user" {
		t.Errorf("message: got %q, want %q", resp["message"], "failed to create user")
	}
}

func TestController_HandleCreateUser_InvalidJSON(t *testing.T) {
	ctrl := NewController(discardLogger(), &mockRepository{})

	body := []byte(`{invalid}`)
	req := httptest.NewRequest(http.MethodPost, "/example/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctrl.handleCreateUser(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}
