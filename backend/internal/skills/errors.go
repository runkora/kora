package skills

import "errors"

// Sentinel errors. The handlers map them to HTTP statuses with errors.Is.
var (
	// ErrMarketplaceNotFound means the tenant declares no Marketplace with
	// this slug (404).
	ErrMarketplaceNotFound = errors.New("marketplace not found")

	// ErrRepositoryNotFound means GitHub has no such repository or ref, or the
	// App cannot see it (404).
	ErrRepositoryNotFound = errors.New("repository or ref not found")

	// ErrSkillNotFound means the Marketplace has no skill at this path (404).
	ErrSkillNotFound = errors.New("skill not found")

	// ErrFileNotFound means the skill lists no file at this path (404).
	ErrFileNotFound = errors.New("skill file not found")

	// ErrCredentialUnavailable means the Marketplace's App key is missing,
	// invalid, or rejected by GitHub (502).
	ErrCredentialUnavailable = errors.New("github app credential unavailable")

	// ErrGitHubFailure means GitHub could not serve the request (502).
	ErrGitHubFailure = errors.New("github failure")

	// ErrInvalidPage means offset or limit is not a valid number (400).
	ErrInvalidPage = errors.New("invalid offset or limit")
)
