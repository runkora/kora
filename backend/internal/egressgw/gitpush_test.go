package egressgw

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func pkt(payload string) string { return fmt.Sprintf("%04x%s", len(payload)+4, payload) }

// sideBandReport builds a side-band receive-pack response whose single ref
// update is rejected with reason.
func sideBandReport(reason string) string {
	inner := pkt("unpack ok\n") + pkt("ng refs/heads/x "+reason+"\n") + "0000"
	return pkt("\x01"+inner) + "0000"
}

func TestExplainWorkflowRefusal(t *testing.T) {
	refused := sideBandReport("refusing to allow a GitHub App to create or update workflow `.github/workflows/test.yml` without `workflows` permission")
	tests := []struct {
		name, method, path, body string
		wantNote                 bool
	}{
		{"workflow refusal", http.MethodPost, "/o/r.git/git-receive-pack", refused, true},
		{"other rejection", http.MethodPost, "/o/r.git/git-receive-pack", sideBandReport("hook declined"), false},
		{"accepted push", http.MethodPost, "/o/r.git/git-receive-pack", pkt("\x01"+pkt("unpack ok\n")+pkt("ok refs/heads/x\n")+"0000") + "0000", false},
		{"no side-band", http.MethodPost, "/o/r.git/git-receive-pack", pkt("unpack ok\n") + pkt("ng refs/heads/x without `workflows` permission\n") + "0000", false},
		{"not receive-pack", http.MethodGet, "/o/r.git/info/refs", refused, false},
	}
	p := &ProxyHandler{brand: testBrand, logger: logging.NewLoggerWithOutput(io.Discard)}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := &http.Response{StatusCode: 200, Header: http.Header{}, Request: req, Body: io.NopCloser(strings.NewReader(tc.body))}
			if err := p.explainWorkflowRefusal(rec); err != nil {
				t.Fatal(err)
			}
			got, _ := io.ReadAll(rec.Body)
			hasNote := strings.Contains(string(got), "workflow files are out of scope")
			if hasNote != tc.wantNote {
				t.Fatalf("note present = %v, want %v; body %q", hasNote, tc.wantNote, got)
			}
			if !tc.wantNote {
				if string(got) != tc.body {
					t.Fatalf("body changed: %q", got)
				}
				return
			}
			if !strings.HasPrefix(string(got), tc.body[:len(tc.body)-4]) || !strings.HasSuffix(string(got), "0000") {
				t.Fatalf("original report not preserved around the note: %q", got)
			}
			note := string(got)[len(tc.body)-4 : len(got)-4]
			if want := fmt.Sprintf("%04x", len(note)); note[:4] != want || note[4] != 2 {
				t.Fatalf("note packet header = %q, want length %s band 2", note[:5], want)
			}
			if rec.ContentLength != int64(len(got)) {
				t.Fatalf("ContentLength = %d, want %d", rec.ContentLength, len(got))
			}
		})
	}
}

func TestExplainWorkflowRefusalOversizePassThrough(t *testing.T) {
	body := pkt("\x01"+strings.Repeat("a", maxReceivePackReport+100)) + "0000"
	req := httptest.NewRequest(http.MethodPost, "/o/r.git/git-receive-pack", nil)
	resp := &http.Response{StatusCode: 200, Header: http.Header{}, Request: req, Body: io.NopCloser(strings.NewReader(body))}
	p := &ProxyHandler{brand: testBrand, logger: logging.NewLoggerWithOutput(io.Discard)}
	if err := p.explainWorkflowRefusal(resp); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("oversize body altered: got %d bytes, want %d", len(got), len(body))
	}
}
