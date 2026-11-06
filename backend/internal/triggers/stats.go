package triggers

import (
	"slices"
	"time"
)

// Trigger stats windows. Buckets align to UTC two-hour boundaries from the one
// holding now-24h to the one holding now, so the first and last can be partial.
const (
	statsWindow      = 24 * time.Hour
	statsBucketWidth = 2 * time.Hour
	statsBucketCount = 13
)

// TriggerStats is one trigger's activity: its outcome rows, plus deliveries
// recorded against it with no outcome of their own (recordedStatuses).
type TriggerStats struct {
	// LastFiredAt is the newest delivery this trigger attempted a run for, over
	// all history. Declined outcomes (filtered, superseded, unrouted, paused) do
	// not count.
	LastFiredAt   *time.Time `json:"last_fired_at"`
	Deliveries24h int        `json:"deliveries_24h"`
	Started24h    int        `json:"started_24h"`
	Failed24h     int        `json:"failed_24h"`
	// Status24h counts the last 24h by DeliveryStatusFilters value, the same
	// match ?status= applies to a trigger's deliveries.
	Status24h map[string]int `json:"status_24h"`
	Buckets   []StatsBucket  `json:"buckets"`
}

// StatsBucket is one two-hour slice of a trigger's recent outcomes.
type StatsBucket struct {
	Start   time.Time `json:"start"`
	Started int       `json:"started"`
	Failed  int       `json:"failed"`
	Other   int       `json:"other"`
}

// TenantTriggerStats is the tenant's trigger activity over the last 24h.
// Started and failed count per-trigger outcomes, as TriggerStats does.
type TenantTriggerStats struct {
	Deliveries24h int `json:"deliveries_24h"`
	Started24h    int `json:"started_24h"`
	Failed24h     int `json:"failed_24h"`
	// DispatchP50Ms and DispatchP95Ms measure delivery arrival to run start over
	// started outcomes. Nil when nothing started.
	DispatchP50Ms *float64 `json:"dispatch_p50_ms"`
	DispatchP95Ms *float64 `json:"dispatch_p95_ms"`
}

// emptyTriggerStats is a trigger with no activity in the window: every count
// present and zero, every bucket present.
func emptyTriggerStats(now time.Time) TriggerStats {
	first := now.UTC().Add(-statsWindow).Truncate(statsBucketWidth)
	st := TriggerStats{Status24h: map[string]int{}, Buckets: make([]StatsBucket, statsBucketCount)}
	for i := range st.Buckets {
		st.Buckets[i].Start = first.Add(time.Duration(i) * statsBucketWidth)
	}
	for v := range DeliveryStatusFilters {
		st.Status24h[v] = 0
	}
	return st
}

// count adds n outcomes of status to the 24h counts and to the bucket.
func (st *TriggerStats) count(status string, bucket, n int) {
	started, failed := slices.Contains(StartedRunStatuses, status), slices.Contains(FailureRunStatuses, status)
	st.Deliveries24h += n
	if started {
		st.Started24h += n
	}
	if failed {
		st.Failed24h += n
	}
	for v, outcomes := range DeliveryStatusFilters {
		if status == v || slices.Contains(outcomes, status) {
			st.Status24h[v] += n
		}
	}
	if bucket < 0 || bucket >= len(st.Buckets) {
		return
	}
	switch b := &st.Buckets[bucket]; {
	case started:
		b.Started += n
	case failed:
		b.Failed += n
	default:
		b.Other += n
	}
}
