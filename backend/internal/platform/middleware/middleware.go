package middleware

import (
	"net/http"
)

type MiddlewareHandler func(http.Handler) http.Handler
type Middleware struct {
	next MiddlewareHandler
}

func NewMiddleware(next MiddlewareHandler) Middleware {
	return Middleware{next: next}
}

// Populates the middleware chain in order of middleware provided
// The first middleware provided will be the first to execute
func MiddlewareChain(middleware ...Middleware) MiddlewareHandler {
	return func(next http.Handler) http.Handler {
		for i := len(middleware) - 1; i >= 0; i-- {
			next = middleware[i].next(next)
		}
		return next
	}
}