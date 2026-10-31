package landlockmode

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    Mode
		wantErr bool
	}{
		{in: "", want: Enforce},
		{in: "enforce", want: Enforce},
		{in: "bestEffort", want: BestEffort},
		{in: "besteffort", wantErr: true},
		{in: "off", wantErr: true},
	} {
		got, err := Parse(tc.in)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("Parse(%q) = %q, %v; want %q, error %v", tc.in, got, err, tc.want, tc.wantErr)
		}
	}
}

// Enforce refuses a kernel below the required ABI and names the setting that
// relaxes it. bestEffort accepts any kernel.
func TestCheck(t *testing.T) {
	for _, tc := range []struct {
		mode    Mode
		abi     int
		wantErr bool
	}{
		{Enforce, -1, true},
		{Enforce, 0, true},
		{Enforce, 1, true},
		{Enforce, 2, false},
		{Enforce, 6, false},
		{BestEffort, -1, false},
		{BestEffort, 1, false},
	} {
		err := tc.mode.Check(tc.abi)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s.Check(%d) = %v, want error %v", tc.mode, tc.abi, err, tc.wantErr)
		}
		if err != nil && !strings.Contains(err.Error(), "agents.landlock") {
			t.Errorf("error %q does not name agents.landlock", err)
		}
	}
}

func TestMissing(t *testing.T) {
	for abi, want := range map[int]int{0: 3, 2: 2, 3: 1, 4: 1, 6: 0} {
		if got := Missing(abi); len(got) != want {
			t.Errorf("Missing(%d) = %v, want %d entries", abi, got, want)
		}
	}
}
