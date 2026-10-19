package egressgw

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// workflowRefusalMarker is the phrase GitHub puts in a rejected ref's status
// when the installation token lacks the workflows permission.
const workflowRefusalMarker = "without `workflows` permission"

// maxReceivePackReport bounds the response buffered for inspection. A
// receive-pack response is a short report; anything larger passes through.
const maxReceivePackReport = 1 << 20

// explainWorkflowRefusal is the proxy's ModifyResponse. When GitHub refuses a
// push for lack of the workflows permission, it appends a side-band message
// saying the refusal is deliberate, so the agent does not read it as a fault
// to retry. Every other response is returned untouched.
func (p *ProxyHandler) explainWorkflowRefusal(resp *http.Response) error {
	req := resp.Request
	if req == nil || req.Method != http.MethodPost || !strings.HasSuffix(req.URL.Path, "/git-receive-pack") ||
		resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Encoding") != "" {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReceivePackReport+1))
	if err != nil {
		return fmt.Errorf("read receive-pack report: %w", err)
	}
	if len(body) > maxReceivePackReport {
		resp.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(body), resp.Body), resp.Body}
		return nil
	}
	_ = resp.Body.Close()

	msg := fmt.Sprintf("%s: workflow files are out of scope for agent pushes. GitHub refused this push because it changes .github/workflows, and this connection does not mint the workflows permission by design. Remove the workflow change from the commit.\n",
		p.brand.ProductName())
	body = appendSideBandMessage(body, msg)
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", fmt.Sprint(len(body)))
	return nil
}

// appendSideBandMessage inserts msg as a band-2 packet before the closing
// flush of a side-band receive-pack report that carries the workflows refusal.
// Any other body is returned as is.
func appendSideBandMessage(body []byte, msg string) []byte {
	const flush = "0000"
	// A report without side-band starts with an "unpack" line, not a band byte.
	if len(body) < 5 || body[4] < 1 || body[4] > 3 ||
		!bytes.HasSuffix(body, []byte(flush)) || !bytes.Contains(body, []byte(workflowRefusalMarker)) {
		return body
	}
	out := make([]byte, 0, len(body)+len(msg)+5)
	out = append(out, body[:len(body)-len(flush)]...)
	out = fmt.Appendf(out, "%04x\x02%s", len(msg)+5, msg)
	return append(out, flush...)
}
