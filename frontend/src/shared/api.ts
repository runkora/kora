/**
 * API type definitions mirroring the Go backend's response shapes.
 *
 * The Go backend returns raw JSON — there is no { data } envelope.
 * Update these types whenever the backend contract changes.
 */

// ---------------------------------------------------------------------------
// Example domain  (internal/example)
// ---------------------------------------------------------------------------

/** GET /example/health */
export interface HealthResponse {
  status: string;
  service: string;
}
