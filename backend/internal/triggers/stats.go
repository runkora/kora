package triggers

import "time"

// StatsWindow is a span of trigger stats. Buckets align to UTC multiples of
// BucketWidth with date_bin, from the one holding now-Span to the one holding now.
type StatsWindow struct {
	Name        string
	Span        time.Duration
	BucketWidth time.Duration
}

// The windows GET /api/v1/triggers accepts as ?window=.
var (
	StatsWindow24h = StatsWindow{Name: "24h", Span: 24 * time.Hour, BucketWidth: 2 * time.Hour}
	StatsWindow7d  = StatsWindow{Name: "7d", Span: 7 * 24 * time.Hour, BucketWidth: 12 * time.Hour}
)

// ParseStatsWindow reads ?window=. Empty is 24h; any other value is not a window.
func ParseStatsWindow(raw string) (StatsWindow, bool) {
	switch raw {
	case "", StatsWindow24h.Name:
		return StatsWindow24h, true
	case StatsWindow7d.Name:
		return StatsWindow7d, true
	}
	return StatsWindow{}, false
}

// bucketCount is the buckets from the one holding now-Span to the one holding now.
func (w StatsWindow) bucketCount() int {
	return int(w.Span/w.BucketWidth) + 1
}

// firstBucket is the start of the bucket holding now-Span. Go truncates from
// the zero time, which shares UTC day boundaries with date_bin's origin.
func (w StatsWindow) firstBucket(now time.Time) time.Time {
	return now.UTC().Add(-w.Span).Truncate(w.BucketWidth)
}

// TriggerStats is one trigger's activity: its outcome rows, plus deliveries
// recorded against it with no outcome of their own (recordedStatuses). Every
// count but LastFiredAt covers the requested window.
type TriggerStats struct {
	// LastFiredAt is the newest delivery this trigger attempted a run for, over
	// all history. Declined outcomes (filtered, superseded, unrouted, paused) do
	// not count.
	LastFiredAt *time.Time `json:"last_fired_at"`
	Deliveries  int        `json:"deliveries"`
	Started     int        `json:"started"`
	Failed      int        `json:"failed"`
	// StatusCounts counts by trigger status, the same match ?status= applies to
	// a trigger's deliveries. The counts sum to Deliveries.
	StatusCounts map[string]int `json:"status_counts"`
	Buckets      []StatsBucket  `json:"buckets"`
}

// StatsBucket is one slice of a trigger's outcomes in the window.
type StatsBucket struct {
	Start   time.Time `json:"start"`
	Started int       `json:"started"`
	Failed  int       `json:"failed"`
	Other   int       `json:"other"`
}

// TenantTriggerStats is the tenant's trigger activity over the window.
// Started and failed count per-trigger outcomes, as TriggerStats does.
type TenantTriggerStats struct {
	Deliveries int `json:"deliveries"`
	Started    int `json:"started"`
	Failed     int `json:"failed"`
	// DispatchP50Ms and DispatchP95Ms measure delivery arrival to run start over
	// started outcomes. Nil when nothing started.
	DispatchP50Ms *float64 `json:"dispatch_p50_ms"`
	DispatchP95Ms *float64 `json:"dispatch_p95_ms"`
}

// emptyTriggerStats is a trigger with no activity in the window: every count
// present and zero, every bucket present.
func emptyTriggerStats(now time.Time, w StatsWindow) TriggerStats {
	first := w.firstBucket(now)
	st := TriggerStats{StatusCounts: map[string]int{}, Buckets: make([]StatsBucket, w.bucketCount())}
	for i := range st.Buckets {
		st.Buckets[i].Start = first.Add(time.Duration(i) * w.BucketWidth)
	}
	for _, v := range TriggerStatuses {
		st.StatusCounts[v] = 0
	}
	return st
}

// count adds n outcomes of status to the window's counts and to the bucket.
func (st *TriggerStats) count(status string, bucket, n int) {
	ts := triggerStatusOf[status]
	st.Deliveries += n
	st.StatusCounts[ts] += n
	switch ts {
	case TriggerStatusStarted:
		st.Started += n
	case TriggerStatusFailed:
		st.Failed += n
	}
	if bucket < 0 || bucket >= len(st.Buckets) {
		return
	}
	switch b := &st.Buckets[bucket]; ts {
	case TriggerStatusStarted:
		b.Started += n
	case TriggerStatusFailed:
		b.Failed += n
	default:
		b.Other += n
	}
}
