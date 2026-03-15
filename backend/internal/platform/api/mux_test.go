package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/middleware"
)

// TestNewMux verifies Mux creation
func TestNewMux(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()

	mux := NewMux(ctx, logger)
	if mux == nil {
		t.Fatal("NewMux() returned nil")
	}

	if mux.ServeMux == nil {
		t.Error("Mux ServeMux is nil")
	}
}

// TestMuxHandle verifies basic route handling
func TestMuxHandle(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	mux := NewMux(ctx, logger)
	mux.SetBasePath("/api")

	called := false
	mux.Handle("GET /test", func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler was not called")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Response code = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestMuxHandleMultipleRoutes verifies multiple route registration
func TestMuxHandleMultipleRoutes(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	mux := NewMux(ctx, logger)
	mux.SetBasePath("/api")

	route1Called := false
	route2Called := false
	route3Called := false

	mux.Handle("GET /route1", func(w http.ResponseWriter, r *http.Request) {
		route1Called = true
		w.WriteHeader(http.StatusOK)
	})

	mux.Handle("POST /route2", func(w http.ResponseWriter, r *http.Request) {
		route2Called = true
		w.WriteHeader(http.StatusCreated)
	})

	mux.Handle("DELETE /route3", func(w http.ResponseWriter, r *http.Request) {
		route3Called = true
		w.WriteHeader(http.StatusNoContent)
	})

	// Test route1
	req1 := httptest.NewRequest("GET", "/api/route1", nil)
	rec1 := httptest.NewRecorder()
	mux.ServeHTTP(rec1, req1)

	if !route1Called {
		t.Error("Route 1 handler was not called")
	}

	// Test route2
	req2 := httptest.NewRequest("POST", "/api/route2", nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)

	if !route2Called {
		t.Error("Route 2 handler was not called")
	}

	// Test route3
	req3 := httptest.NewRequest("DELETE", "/api/route3", nil)
	rec3 := httptest.NewRecorder()
	mux.ServeHTTP(rec3, req3)

	if !route3Called {
		t.Error("Route 3 handler was not called")
	}
}

// TestMuxSetMiddleware verifies middleware registration
func TestMuxSetMiddleware(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	mux := NewMux(ctx, logger)
	mux.SetBasePath("/api")

	called := false
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			next.ServeHTTP(w, r)
		})
	}

	mux.SetMiddleware(mw)

	mux.Handle("GET /test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if !called {
		t.Error("Middleware was not called")
	}
}

// TestMuxMiddlewareOrdering verifies middleware execution order
func TestMuxMiddlewareOrdering(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	mux := NewMux(ctx, logger)
	mux.SetBasePath("/api")

	var executionOrder []string

	controllerMw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			executionOrder = append(executionOrder, "controller-mw-before")
			next.ServeHTTP(w, r)
			executionOrder = append(executionOrder, "controller-mw-after")
		})
	}

	routeMw := middleware.NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			executionOrder = append(executionOrder, "route-mw-before")
			next.ServeHTTP(w, r)
			executionOrder = append(executionOrder, "route-mw-after")
		})
	})

	mux.SetMiddleware(controllerMw)

	mux.HandleWithMiddleware("GET /test", middleware.MiddlewareChain(routeMw), func(w http.ResponseWriter, r *http.Request) {
		executionOrder = append(executionOrder, "handler")
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// Verify execution order: controller-mw → route-mw → handler → route-mw → controller-mw
	expected := []string{
		"controller-mw-before", "route-mw-before", "handler", "route-mw-after", "controller-mw-after",
	}

	if len(executionOrder) != len(expected) {
		t.Fatalf("Execution order length = %d, want %d", len(executionOrder), len(expected))
	}

	for i, step := range expected {
		if executionOrder[i] != step {
			t.Errorf("executionOrder[%d] = %s, want %s", i, executionOrder[i], step)
		}
	}
}

// TestMuxServeHTTP verifies HTTP request handling
func TestMuxServeHTTP(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	mux := NewMux(ctx, logger)
	mux.SetBasePath("/api")

	mux.Handle("GET /test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Response code = %d, want %d", rec.Code, http.StatusOK)
	}

	if rec.Body.String() != "success" {
		t.Errorf("Response body = %s, want 'success'", rec.Body.String())
	}
}

// TestMuxSetBasePath verifies base path functionality
func TestMuxSetBasePath(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	mux := NewMux(ctx, logger)
	mux.SetBasePath("/v1/api")

	mux.Handle("GET /users", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Should work with base path
	req := httptest.NewRequest("GET", "/v1/api/users", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Response code = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestMuxNotFoundHandler verifies 404 handling
func TestMuxNotFoundHandler(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	mux := NewMux(ctx, logger)
	mux.SetBasePath("/api")

	// Don't register any routes - should 404
	req := httptest.NewRequest("GET", "/api/nonexistent", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Response code = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestMuxMethodNotAllowed verifies 405 handling
func TestMuxMethodNotAllowed(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	mux := NewMux(ctx, logger)
	mux.SetBasePath("/api")

	// Register GET route only
	mux.Handle("GET /resource", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Try POST to GET-only route - ServeMux will return 405
	req := httptest.NewRequest("POST", "/api/resource", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	// Should return 405 Method Not Allowed
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Response code = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// TestMuxWithNoRoutes verifies behavior with no registered routes
func TestMuxWithNoRoutes(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	mux := NewMux(ctx, logger)
	mux.SetBasePath("/api")

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()

	// Should not panic
	mux.ServeHTTP(rec, req)

	// Should return 404
	if rec.Code != http.StatusNotFound {
		t.Errorf("Response code = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestMuxMiddlewareWithNoRoutes verifies 404 response when no routes match
func TestMuxMiddlewareWithNoRoutes(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	mux := NewMux(ctx, logger)
	mux.SetBasePath("/api")

	called := false
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			next.ServeHTTP(w, r)
		})
	}

	mux.SetMiddleware(mw)

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	// Note: In this Mux implementation, middleware is applied per registered route.
	// Since no routes are registered, we expect a 404 and middleware not to be called.
	if rec.Code != http.StatusNotFound {
		t.Errorf("Response code = %d, want %d", rec.Code, http.StatusNotFound)
	}

	// Middleware should not be called when no routes match
	if called {
		t.Error("Middleware should not be called when no routes match")
	}
}

// TestMuxConcurrentRequests verifies thread safety (smoke test)
func TestMuxConcurrentRequests(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	mux := NewMux(ctx, logger)
	mux.SetBasePath("/api")

	mux.Handle("GET /test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Make concurrent requests
	done := make(chan bool, 10)
	for range 10 {
		go func() {
			req := httptest.NewRequest("GET", "/api/test", nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			done <- true
		}()
	}

	// Wait for all requests
	for range 10 {
		<-done
	}

	// Just verify we didn't panic
}

// TestMuxHandleNilHandler verifies panic on nil handler
func TestMuxHandleNilHandler(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	mux := NewMux(ctx, logger)
	mux.SetBasePath("/api")

	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic when handler is nil")
		}
	}()

	// This should panic
	mux.Handle("GET /test", nil)
}

// TestMuxRootBasePath verifies root base path handling
func TestMuxRootBasePath(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	mux := NewMux(ctx, logger)
	mux.SetBasePath("/")

	mux.Handle("GET /test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Response code = %d, want %d", rec.Code, http.StatusOK)
	}
}
