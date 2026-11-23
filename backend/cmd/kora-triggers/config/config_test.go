package triggersConfig

import "testing"

// A pre-wake lead is 0 or between one minute and kora-agents' limit.
func TestValidatePreWakeLead(t *testing.T) {
	for lead, ok := range map[int]bool{0: true, 60: true, 180: true, 900: true, -1: false, 30: false, 901: false} {
		var cfg Config
		cfg.Scheduler.PreWakeLeadSecs = lead
		if err := Validate(&cfg); (err == nil) != ok {
			t.Errorf("Validate(%d) = %v, want ok %v", lead, err, ok)
		}
	}
}
