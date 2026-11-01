package identityproxy

import "errors"

// Static span statuses and error.type values for failed spans.
var (
	errSpanContextCanceledDuringRetry     = errors.New("context canceled during retry")
	errSpanContextCanceledWaitingForEnvoy = errors.New("context canceled waiting for Envoy")
)
