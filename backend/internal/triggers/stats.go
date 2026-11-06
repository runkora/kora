package triggers

import (
	"slices"
	"time"
)

// Trigger stats windows. Buckets align to UTC two-hour boundaries, so the last
// bucket is the partial one holding now.
const (
	statsWindow      = 24 * time.Hour
	statsBucketWidth = 2 * time.Hour
	statsBucketCount = 12
)

// TriggerStats is one trigger's activity, counted per delivery that reached it
// by the trigger's own outcome, or the delivery's status when it has none.
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
	first := now.UTC().Truncate(statsBucketWidth).Add(-(statsBucketCount - 1) * statsBucketWidth)
	st := TriggerStats{Status24h: map[string]int{}, Buckets: make([]StatsBucket, statsBucketCount)}
	for i := range st.Buckets {
		st.Buckets[i].Start = first.Add(time.Duration(i) * statsBucketWidth)
	}
	for v := range DeliveryStatusFilters {
		st.Status24h[v] = 0
	}
	return st
}

// count adds n outcomes of status to the 24h counts and, when bucket is not -1,
// to that bucket.
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
