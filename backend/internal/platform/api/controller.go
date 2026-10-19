package api

import mw "github.com/runkora/kora/backend/internal/platform/middleware"

type Controller interface {
	GroupedBy() string
	Middleware() mw.MiddlewareHandler
	Boot(*Mux)
}
