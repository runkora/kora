package grading

import "testing"

// verdicts is n copies of v.
func verdicts(n int, v Verdict) []Verdict {
	out := make([]Verdict, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func TestWindow(t *testing.T) {
	ninety := new(0.9)
	tests := []struct {
		name   string
		min    *float64
		in     []Verdict
		status PassRateStatus
		passed int
		graded int
	}{
		{"no minimum still counts", nil, verdicts(20, Pass), NoMinimum, 20, 20},
		{"no minimum with few runs", nil, verdicts(3, Pass), NoMinimum, 3, 3},
		{"no runs", ninety, nil, NotEnoughRuns, 0, 0},
		{"14 runs is not enough", ninety, verdicts(14, Pass), NotEnoughRuns, 14, 14},
		{"15 runs gives a status", ninety, verdicts(15, Pass), Passing, 15, 15},
		{"15 runs under the minimum fails", ninety, append(verdicts(13, Pass), verdicts(2, Fail)...), Failing, 13, 15},
		{"at the minimum passes", ninety, append(verdicts(27, Pass), verdicts(3, Fail)...), Passing, 27, 30},
		{"under the minimum fails", ninety, append(verdicts(26, Pass), verdicts(4, Fail)...), Failing, 26, 30},
		{"uncertain counts in the denominator", ninety, append(verdicts(26, Pass), verdicts(4, Uncertain)...), Failing, 26, 30},
		{"only the newest 30 count", ninety, append(verdicts(30, Pass), verdicts(10, Fail)...), Passing, 30, 30},
		{"older passes past 30 do not count", ninety, append(append(verdicts(25, Pass), verdicts(5, Fail)...), verdicts(10, Pass)...), Failing, 25, 30},
		{"zero minimum always passes", new(float64(0)), verdicts(15, Fail), Passing, 0, 15},
		{"full minimum needs every run", new(float64(1)), append(verdicts(29, Pass), Fail), Failing, 29, 30},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Window(tc.min, tc.in)
			want := PassRate{Status: tc.status, Passed: tc.passed, Graded: tc.graded}
			if got != want {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}
