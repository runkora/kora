package bundle

import (
	"fmt"
	"net/http"
	"time"
)

// writeBundle serves a compiled bundle as the gzip tarball OPA downloads.
// api.JSON would encode the bytes, so the status is written directly here.
func writeBundle(w http.ResponseWriter, cached *CachedBundle) {
	w.Header().Set("Content-Type", "application/vnd.openpolicyagent.bundle")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(cached.Data)))
	w.Header().Set("ETag", fmt.Sprintf("%q", cached.Revision))
	w.Header().Set("Last-Modified", cached.CompiledAt.Format(time.RFC1123))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(cached.Data)
}
