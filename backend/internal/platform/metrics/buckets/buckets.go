// Package buckets defines named histogram bucket-boundary presets for OTel
// Float64Histogram instruments that record durations in seconds.
//
// The OTel SDK's default histogram boundaries are tuned for millisecond-scale
// values (roughly 0 to 10000), so a histogram recording seconds without
// explicit boundaries collapses nearly every real sample into a single
// bucket. Every duration histogram across the platform must pass one of
// these presets via metric.WithExplicitBucketBoundaries instead of relying
// on the SDK default.
//
// This package deliberately has zero internal imports, so both
// internal/platform/metrics and internal/platform/middleware (which
// internal/platform/metrics already imports) can depend on it without
// creating an import cycle.
package buckets

// Fast suits an operation that is typically sub-millisecond and rarely
// exceeds a second, such as a warm, in-process policy check. Boundaries
// start at 100 microseconds so the bulk of a hot path does not collapse into
// the first (le="+Inf"-adjacent) bucket.
var Fast = []float64{.0001, .00025, .0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1}

// Default is Prometheus client_golang's conventional DefBuckets: general
// HTTP and other short-duration work, 5ms to 10s.
var Default = []float64{.005, .01, .025, .05, .075, .1, .25, .5, .75, 1, 2.5, 5, 7.5, 10}

// Long suits an operation that can legitimately run from seconds to hours,
// such as an agent run or a browser's live session duration. Boundaries span
// 1s up through roughly 24h, with intermediate buckets at the minute, tens-
// of-minutes, and hour scale.
var Long = []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600, 7200, 14400, 43200, 86400}
