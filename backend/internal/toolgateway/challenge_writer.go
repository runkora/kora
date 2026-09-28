package toolgateway

import "net/http"

// challengeWriter injects the WWW-Authenticate header iff the wrapped handler
// writes a 401, without disturbing any other status.
type challengeWriter struct {
	http.ResponseWriter
	challenge string
	wrote     bool
}

func (w *challengeWriter) WriteHeader(code int) {
	if !w.wrote {
		w.wrote = true
		if code == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", w.challenge)
		}
	}
	w.ResponseWriter.WriteHeader(code)
}
