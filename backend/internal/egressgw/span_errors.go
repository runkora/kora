package egressgw

import "errors"

// Static span statuses and error.type values for failed spans.
var (
	errSpanNodeOwnerLookup       = errors.New("node owner lookup")
	errSpanRepositoryOwnerLookup = errors.New("repository owner lookup")
)
