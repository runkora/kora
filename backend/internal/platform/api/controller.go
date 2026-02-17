package api

import mw "github.com/clear-route/orbit/internal/platform/middleware"

type Controller interface {
	GroupedBy() string
	Middleware() mw.MiddlewareHandler
	Boot(*Mux)
}
