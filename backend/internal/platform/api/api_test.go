package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
)

// mockController implements the Controller interface for testing
type mockController struct {
	basePath   string
	middleware mw.MiddlewareHandler
	bootCalled bool
}

func (m *mockController) GroupedBy() string {
	return m.basePath
}

func (m *mockController) Middleware() mw.MiddlewareHandler {
	return m.middleware
}

func (m *mockController) Boot(mux *Mux) {
	m.bootCalled = true
	// Register a test route
	mux.Handle("GET /test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("controller response"))
	})
}

// TestNewAPI verifies API creation
func TestNewAPI(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()

	api := NewAPI(ctx, logger)

	if api == nil {
		t.Fatal("NewAPI() returned nil")
	}

	if api.logger == nil {
		t.Error("API logger is nil")
	}

	if api.mux == nil {
		t.Error("API mux is nil")
	}
}

// TestServerCreation verifies basic server creation
func TestServerCreation(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	api := NewAPI(ctx, logger)

	server := api.Server(ctx, 8080, "/", nil)

	if server == nil {
		t.Fatal("Server() returned nil")
	}

	if server.Addr != ":8080" {
		t.Errorf("Server.Addr = %s, want :8080", server.Addr)
	}

	if server.Handler == nil {
		t.Error("Server.Handler is nil")
	}
}

// TestServerWithCustomPort verifies server creation with custom port
func TestServerWithCustomPort(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	api := NewAPI(ctx, logger)

	testCases := []struct {
		name string
		port int
		want string
	}{
		{"default port", 8080, ":8080"},
		{"custom port", 3000, ":3000"},
		{"high port", 9999, ":9999"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := api.Server(ctx, tc.port, "/", nil)

			if server.Addr != tc.want {
				t.Errorf("Server.Addr = %s, want %s", server.Addr, tc.want)
			}
		})
	}
}

// TestHealthCheckEndpoint verifies health check functionality
func TestHealthCheckEndpoint(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	api := NewAPI(ctx, logger)

	server := api.Server(ctx, 8080, "/", nil)

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Health check status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	expected := `{"status": "health ok"}`
	if body != expected {
		t.Errorf("Health check body = %s, want %s", body, expected)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "" && contentType != "application/json" {
		t.Logf("Note: Content-Type = %s (not set by handler)", contentType)
	}
}

// TestHealthCheckWithBasePath verifies health check works with base path
func TestHealthCheckWithBasePath(t *testing.T) {
	testCases := []struct {
		name     string
		basePath string
		reqPath  string
	}{
		{"root base path", "/", "/health"},
		{"api base path", "/api", "/health"},
		{"versioned base path", "/api/v1", "/health"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			logger := logging.NewLogger()
			api := NewAPI(ctx, logger)
			server := api.Server(ctx, 8080, tc.basePath, nil)

			req := httptest.NewRequest("GET", tc.reqPath, nil)
			rec := httptest.NewRecorder()

			server.Handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("Health check status = %d, want %d", rec.Code, http.StatusOK)
			}
		})
	}
}

// TestServerWithController verifies controller registration and routing
func TestServerWithController(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	api := NewAPI(ctx, logger)

	controller := &mockController{
		basePath: "/users",
	}

	server := api.Server(ctx, 8080, "/api", nil, controller)

	// Verify controller was booted
	if !controller.bootCalled {
		t.Error("Controller Boot() was not called")
	}

	// Test controller route
	req := httptest.NewRequest("GET", "/api/users/test", nil)
	rec := httptest.NewRecorder()

	server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Controller route status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if body != "controller response" {
		t.Errorf("Controller route body = %s, want 'controller response'", body)
	}
}

// TestServerWithMultipleControllers verifies multiple controller registration
func TestServerWithMultipleControllers(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	api := NewAPI(ctx, logger)

	controller1 := &mockController{basePath: "/users"}
	controller2 := &mockController{basePath: "/posts"}
	controller3 := &mockController{basePath: "/comments"}

	server := api.Server(ctx, 8080, "/api", nil, controller1, controller2, controller3)

	// Verify all controllers were booted
	if !controller1.bootCalled {
		t.Error("Controller 1 Boot() was not called")
	}
	if !controller2.bootCalled {
		t.Error("Controller 2 Boot() was not called")
	}
	if !controller3.bootCalled {
		t.Error("Controller 3 Boot() was not called")
	}

	// Test each controller's route
	testCases := []struct {
		path string
		want int
	}{
		{"/api/users/test", http.StatusOK},
		{"/api/posts/test", http.StatusOK},
		{"/api/comments/test", http.StatusOK},
	}

	for _, tc := range testCases {
		req := httptest.NewRequest("GET", tc.path, nil)
		rec := httptest.NewRecorder()

		server.Handler.ServeHTTP(rec, req)

		if rec.Code != tc.want {
			t.Errorf("Route %s status = %d, want %d", tc.path, rec.Code, tc.want)
		}
	}
}

// TestServerBasePathStripPrefix verifies base path stripping behavior
func TestServerBasePathStripPrefix(t *testing.T) {
	testCases := []struct {
		name         string
		serverBase   string
		requestPath  string
		expectStatus int
	}{
		{"root base - direct access", "/", "/users/test", http.StatusOK},
		{"api base - strips /api", "/api", "/api/users/test", http.StatusOK},
		{"api base - without prefix fails", "/api", "/users/test", http.StatusNotFound},
		{"versioned base - strips /v1", "/v1", "/v1/users/test", http.StatusOK},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			logger := logging.NewLogger()
			api := NewAPI(ctx, logger)

			controller := &mockController{
				basePath: "/users",
			}

			server := api.Server(ctx, 8080, tc.serverBase, nil, controller)

			req := httptest.NewRequest("GET", tc.requestPath, nil)
			rec := httptest.NewRecorder()

			server.Handler.ServeHTTP(rec, req)

			if rec.Code != tc.expectStatus {
				t.Errorf("Status = %d, want %d", rec.Code, tc.expectStatus)
			}
		})
	}
}

// TestServerMiddlewareOrchestration verifies middleware chain ordering
func TestServerMiddlewareOrchestration(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	api := NewAPI(ctx, logger)

	var executionOrder []string

	// User-provided server-level middleware
	serverMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			executionOrder = append(executionOrder, "server-mw-before")
			next.ServeHTTP(w, r)
			executionOrder = append(executionOrder, "server-mw-after")
		})
	}

	// Controller-level middleware
	controllerMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			executionOrder = append(executionOrder, "controller-mw-before")
			next.ServeHTTP(w, r)
			executionOrder = append(executionOrder, "controller-mw-after")
		})
	}

	controller := &mockController{
		basePath:   "/users",
		middleware: controllerMiddleware,
	}

	server := api.Server(ctx, 8080, "/api", serverMiddleware, controller)

	req := httptest.NewRequest("GET", "/api/users/test", nil)
	rec := httptest.NewRecorder()

	server.Handler.ServeHTTP(rec, req)

	// Expected order: default(empty) → server → controller → handler → controller → server → default
	// Since default is empty, we expect: server → controller → handler → controller → server
	expected := []string{
		"server-mw-before",
		"controller-mw-before",
		// handler executes here
		"controller-mw-after",
		"server-mw-after",
	}

	if len(executionOrder) != len(expected) {
		t.Fatalf("Execution order length = %d, want %d. Got: %v", len(executionOrder), len(expected), executionOrder)
	}

	for i, step := range expected {
		if executionOrder[i] != step {
			t.Errorf("executionOrder[%d] = %s, want %s", i, executionOrder[i], step)
		}
	}
}

// TestServerWithoutControllers verifies server works with no controllers
func TestServerWithoutControllers(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	api := NewAPI(ctx, logger)

	server := api.Server(ctx, 8080, "/", nil)

	// Health check should still work
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Health check status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Other routes should 404
	req = httptest.NewRequest("GET", "/nonexistent", nil)
	rec = httptest.NewRecorder()

	server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Nonexistent route status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestServerMiddlewareWithNilController verifies nil middleware handling
func TestServerMiddlewareWithNilController(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	api := NewAPI(ctx, logger)

	controller := &mockController{
		basePath:   "/users",
		middleware: nil, // No controller middleware
	}

	server := api.Server(ctx, 8080, "/api", nil, controller)

	req := httptest.NewRequest("GET", "/api/users/test", nil)
	rec := httptest.NewRecorder()

	// Should not panic with nil middleware
	server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestHealthCheckBypassesMiddleware verifies that the health endpoint is intentionally
// exempt from server-level middleware (tracing, logging, metrics). Kubernetes probes
// fire every few seconds and would otherwise flood observability pipelines with noise.
func TestHealthCheckBypassesMiddleware(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	a := NewAPI(ctx, logger)

	middlewareCalled := false
	serverMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			middlewareCalled = true
			next.ServeHTTP(w, r)
		})
	}

	server := a.Server(ctx, 8080, "/", serverMiddleware)

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	server.Handler.ServeHTTP(rec, req)

	// Health check must succeed.
	if rec.Code != http.StatusOK {
		t.Errorf("Health check status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Server middleware must NOT be invoked for /health — this is the key invariant.
	if middlewareCalled {
		t.Error("Server middleware must not be called for /health (probe noise suppression)")
	}
}

// TestServerMiddlewareAppliedToAppRoutes verifies that server-level middleware
// IS still applied to real application routes even though /health bypasses it.
func TestServerMiddlewareAppliedToAppRoutes(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	a := NewAPI(ctx, logger)

	middlewareCalled := false
	serverMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			middlewareCalled = true
			next.ServeHTTP(w, r)
		})
	}

	controller := &mockController{basePath: "/users"}
	server := a.Server(ctx, 8080, "/api", serverMiddleware, controller)

	req := httptest.NewRequest("GET", "/api/users/test", nil)
	rec := httptest.NewRecorder()

	server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("App route status = %d, want %d", rec.Code, http.StatusOK)
	}

	if !middlewareCalled {
		t.Error("Server middleware was not called for app route")
	}
}

// TestServerReadHeaderTimeout verifies server timeout configuration
func TestServerReadHeaderTimeout(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	api := NewAPI(ctx, logger)

	server := api.Server(ctx, 8080, "/", nil)

	// Verify ReadHeaderTimeout is set
	if server.ReadHeaderTimeout == 0 {
		t.Error("ReadHeaderTimeout is not set")
	}

	// Should be 10 seconds
	expectedTimeout := 10
	actualSeconds := int(server.ReadHeaderTimeout.Seconds())
	if actualSeconds != expectedTimeout {
		t.Errorf("ReadHeaderTimeout = %d seconds, want %d seconds", actualSeconds, expectedTimeout)
	}
}

// TestHealthCheckErrorHandling verifies error handling in health check
func TestHealthCheckErrorHandling(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	api := NewAPI(ctx, logger)

	server := api.Server(ctx, 8080, "/", nil)

	// Use a ResponseWriter that fails on Write
	req := httptest.NewRequest("GET", "/health", nil)
	rec := &failingResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		writeError:       io.ErrClosedPipe,
	}

	// Should not panic even if Write fails
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Health check panicked: %v", r)
		}
	}()

	server.Handler.ServeHTTP(rec, req)

	// Verify status code was written despite write error
	if rec.Code() != http.StatusOK {
		t.Errorf("Status code = %d, want %d", rec.Code(), http.StatusOK)
	}
}

// failingResponseWriter is a test helper that fails on Write
type failingResponseWriter struct {
	*httptest.ResponseRecorder
	writeError error
	code       int
	header     http.Header
}

func (f *failingResponseWriter) Header() http.Header {
	if f.header == nil {
		f.header = make(http.Header)
	}
	return f.header
}

func (f *failingResponseWriter) WriteHeader(statusCode int) {
	f.code = statusCode
	f.ResponseRecorder.WriteHeader(statusCode)
}

func (f *failingResponseWriter) Write(b []byte) (int, error) {
	if f.writeError != nil {
		return 0, f.writeError
	}
	return f.ResponseRecorder.Write(b)
}

func (f *failingResponseWriter) Code() int {
	if f.code != 0 {
		return f.code
	}
	return f.ResponseRecorder.Code
}
