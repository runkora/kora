package grading

// PassRateWindow is how many of a Grader's latest graded runs minPassRate reads.
const PassRateWindow = 30

// minGradedRuns is the fewest graded runs in the window that give a status.
const minGradedRuns = 15

// PassRateStatus is a Grader's standing against its minPassRate.
type PassRateStatus string

const (
	Passing       PassRateStatus = "passing"
	Failing       PassRateStatus = "failing"
	NotEnoughRuns PassRateStatus = "not_enough_runs"
	NoMinimum     PassRateStatus = "no_minimum"
)

// PassRate is the window's counts and the status they give.
type PassRate struct {
	Status PassRateStatus
	Passed int
	Graded int
}

// Window reads verdicts of graded runs, newest first, over the latest
// PassRateWindow. Uncertain runs count in the denominator.
func Window(minPassRate *float64, verdicts []Verdict) PassRate {
	verdicts = verdicts[:min(len(verdicts), PassRateWindow)]
	r := PassRate{Graded: len(verdicts)}
	for _, v := range verdicts {
		if v == Pass {
			r.Passed++
		}
	}
	switch {
	case minPassRate == nil:
		r.Status = NoMinimum
	case r.Graded < minGradedRuns:
		r.Status = NotEnoughRuns
	case float64(r.Passed) >= *minPassRate*float64(r.Graded)-epsilon:
		r.Status = Passing
	default:
		r.Status = Failing
	}
	return r
}
