package skills

import "errors"

// Sentinel errors. The service layer maps them to HTTP status codes with
// errors.Is, so handlers never inspect error strings. A GitHub outage or
// rate limit is ghclient.ErrUnavailable, which the GitHub client defines
// and wraps.
var (
	// ErrMarketplaceNotFound is returned when the marketplace is not declared
	// for the caller's tenant. It means HTTP 404 (ADR "Status-Code Summary").
	ErrMarketplaceNotFound = errors.New("marketplace not found")

	// ErrSkillNotFound is returned when the skill folder does not exist in
	// the repository. It means HTTP 404 (ADR "Status-Code Summary").
	ErrSkillNotFound = errors.New("skill not found")

	// ErrRedisUnavailable is returned when Redis fails. It means HTTP 503
	// (ADR "Status-Code Summary").
	ErrRedisUnavailable = errors.New("redis unavailable")

	// ErrInvalidCursor is returned when the pagination cursor sent by the
	// client cannot be decoded. It means HTTP 400.
	ErrInvalidCursor = errors.New("invalid cursor")

	// ErrSearchIndexMissing asks the service to rebuild a cold search projection.
	ErrSearchIndexMissing = errors.New("skill search index is not ready")

	// ErrSearchUnavailable is a search-storage failure (HTTP 503).
	ErrSearchUnavailable = errors.New("skill search unavailable")

	// ErrInvalidSearch reports a query exceeding the supported limits (HTTP 400).
	ErrInvalidSearch = errors.New("search exceeds 256 characters or 32 words")
)
