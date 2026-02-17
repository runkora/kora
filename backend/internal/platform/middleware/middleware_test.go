package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNewMiddleware verifies middleware wrapper creation
func TestNewMiddleware(t *testing.T) {
	called := false
	mw := NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			next.ServeHTTP(w, r)
		})
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := mw.next(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if !called {
		t.Error("Middleware was not called")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Response code = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestMiddlewareChain verifies middleware chain creation and ordering
func TestMiddlewareChain(t *testing.T) {
	var executionOrder []string

	// Create three middleware that track execution order
	mw1 := NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			executionOrder = append(executionOrder, "mw1-before")
			next.ServeHTTP(w, r)
			executionOrder = append(executionOrder, "mw1-after")
		})
	})

	mw2 := NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			executionOrder = append(executionOrder, "mw2-before")
			next.ServeHTTP(w, r)
			executionOrder = append(executionOrder, "mw2-after")
		})
	})

	mw3 := NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			executionOrder = append(executionOrder, "mw3-before")
			next.ServeHTTP(w, r)
			executionOrder = append(executionOrder, "mw3-after")
		})
	})

	// Chain the middleware
	chain := MiddlewareChain(mw1, mw2, mw3)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		executionOrder = append(executionOrder, "handler")
		w.WriteHeader(http.StatusOK)
	})

	wrapped := chain(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// Verify execution order: outer middleware executes first
	// Expected: mw1-before → mw2-before → mw3-before → handler → mw3-after → mw2-after → mw1-after
	expected := []string{
		"mw1-before", "mw2-before", "mw3-before", "handler",
		"mw3-after", "mw2-after", "mw1-after",
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

// TestMiddlewareChainEmpty verifies empty middleware chain behavior
func TestMiddlewareChainEmpty(t *testing.T) {
	chain := MiddlewareChain()

	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	wrapped := chain(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler was not called with empty chain")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Response code = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestMiddlewareChainSingle verifies single middleware chain
func TestMiddlewareChainSingle(t *testing.T) {
	called := false
	mw := NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			next.ServeHTTP(w, r)
		})
	})

	chain := MiddlewareChain(mw)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := chain(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if !called {
		t.Error("Middleware was not called")
	}
}

// TestMiddlewareResponseModification verifies middleware can modify response
func TestMiddlewareResponseModification(t *testing.T) {
	// Middleware that adds a header
	headerMw := NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Custom-Header", "test-value")
			next.ServeHTTP(w, r)
		})
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := headerMw.next(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if header := rec.Header().Get("X-Custom-Header"); header != "test-value" {
		t.Errorf("X-Custom-Header = %s, want 'test-value'", header)
	}
}

// TestMiddlewareShortCircuit verifies middleware can short-circuit the chain
func TestMiddlewareShortCircuit(t *testing.T) {
	handlerCalled := false

	// Middleware that short-circuits (doesn't call next)
	authMw := NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Simulate auth failure - don't call next
			w.WriteHeader(http.StatusUnauthorized)
		})
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	wrapped := authMw.next(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if handlerCalled {
		t.Error("Handler should not have been called after middleware short-circuit")
	}

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Response code = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// TestMiddlewareNilHandler verifies that MiddlewareChain handles nil handlers defensively
func TestMiddlewareNilHandler(t *testing.T) {
	mw := NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	})

	// Create a middleware chain and pass nil handler
	chain := MiddlewareChain(mw)
	wrapped := chain(nil)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	// Should not panic - defensive nil check should return 500 error
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d for nil handler, got %d", http.StatusInternalServerError, rec.Code)
	}
}

// TestMiddlewarePassthroughRequest verifies request is passed through correctly
func TestMiddlewarePassthroughRequest(t *testing.T) {
	var receivedMethod string
	var receivedPath string

	mw := NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})

	wrapped := mw.next(handler)
	req := httptest.NewRequest("POST", "/api/users", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if receivedMethod != "POST" {
		t.Errorf("Request method = %s, want POST", receivedMethod)
	}

	if receivedPath != "/api/users" {
		t.Errorf("Request path = %s, want /api/users", receivedPath)
	}
}
