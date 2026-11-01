package compatible

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/runkora/kora/backend/internal/inferencegw"
	"github.com/runkora/kora/backend/internal/inferencegw/providers/convert"
	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	ip "github.com/runkora/kora/backend/internal/platform/inferenceproviders/v1alpha1"
	"github.com/runkora/kora/backend/internal/platform/observability"
	"go.opentelemetry.io/otel/codes"
)

// maxSystemOneBody caps the upstream answer read into memory. A body that
// reaches it is refused rather than truncated.
const maxSystemOneBody = 1 << 20 // 1 MiB

// An error body is read up to maxSystemOneErrorBody so a long validation error
// still parses. The message kept from it is cut to maxSystemOneErrorMessage.
const (
	maxSystemOneErrorBody    = 64 << 10
	maxSystemOneErrorMessage = 1 << 10
)

// errMalformedBody is returned when the request body is not a JSON object.
var errMalformedBody = errors.New("systemone: request body is not a JSON object")

// Span descriptions for upstream failures. They are static so traces group.
var (
	errSystemOneStatus   = errors.New("systemone: unexpected status")
	errSystemOneReadBody = errors.New("systemone: reading response body failed")
	errSystemOneTooLarge = errors.New("systemone: response body too large")
)

// SystemOne passes a System One request through to <baseURL>/systemone. Only
// the body's model is rewritten, to the target's model id; every other byte is
// forwarded as received, and the upstream body is returned unchanged.
func (c *Connector) SystemOne(ctx context.Context, req inferencegw.SystemOneRequest) (*inferencegw.SystemOneResult, error) {
	if !c.serves[ip.APISystemOne] {
		return nil, c.unsupported(ip.APISystemOne)
	}
	body, err := withModel(req.Body, req.Model)
	if err != nil {
		return nil, &inferencegw.ConnectorError{
			Provider:        c.name,
			Code:            "INVALID_REQUEST",
			Underlying:      err,
			ProviderMessage: err.Error(),
			HTTPStatus:      http.StatusBadRequest,
		}
	}
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Set("Accept", "application/json")
	if c.apiKey != "" {
		header.Set("Authorization", "Bearer "+c.apiKey)
	}

	_, span, resp, err := c.decide.Do(ctx, httpclient.Request{
		Method: http.MethodPost, BaseURL: c.baseURL, Path: "/systemone", Header: header, Body: bytes.NewReader(body),
	})
	defer span.End()
	if err != nil {
		return nil, c.transportError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxSystemOneErrorBody))
		msg := upstreamMessage(string(raw))
		if len(msg) > maxSystemOneErrorMessage {
			msg = strings.ToValidUTF8(msg[:maxSystemOneErrorMessage], "")
		}
		span.SetAttributes(observability.ErrorUpstreamMessageKey.String(msg))
		span.RecordError(fmt.Errorf("%w: status %d: %s", errSystemOneStatus, resp.StatusCode, msg))
		span.SetStatus(codes.Error, errSystemOneStatus.Error())
		return nil, c.systemOneError(resp.StatusCode, msg)
	}
	out, err := io.ReadAll(io.LimitReader(resp.Body, maxSystemOneBody))
	if err != nil {
		span.RecordError(fmt.Errorf("%w: %w", errSystemOneReadBody, err))
		span.SetStatus(codes.Error, errSystemOneReadBody.Error())
		return nil, c.transportError(ctx, err)
	}
	if len(out) >= maxSystemOneBody {
		span.RecordError(errSystemOneTooLarge)
		span.SetStatus(codes.Error, errSystemOneTooLarge.Error())
		return nil, &inferencegw.ConnectorError{
			Provider:        c.name,
			Code:            "PROVIDER_ERROR",
			Underlying:      errSystemOneTooLarge,
			ProviderMessage: fmt.Sprintf("provider %s answered with %d bytes or more", c.name, maxSystemOneBody),
		}
	}
	in, outTokens := usageTokens(out)
	return &inferencegw.SystemOneResult{Body: out, StatusCode: resp.StatusCode, InputTokens: in, OutputTokens: outTokens}, nil
}

// transportError classifies a failure to complete the request. When the
// caller's context is still live, a deadline is the decision timeout firing:
// the upstream was too slow, so it is a retryable provider error. A cancelled
// caller, or any other failure, keeps the shared classification.
func (c *Connector) transportError(ctx context.Context, err error) error {
	var netErr net.Error
	if ctx.Err() == nil && (errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout())) {
		return &inferencegw.ConnectorError{
			Provider:        c.name,
			Code:            "PROVIDER_ERROR",
			Retryable:       true,
			Underlying:      fmt.Errorf("no response within %s: %w", decisionTimeout, err),
			ProviderMessage: fmt.Sprintf("provider %s did not answer within %s", c.name, decisionTimeout),
		}
	}
	return convert.MapOpenAIError(c.name, err)
}

// systemOneError classifies an upstream failure. 400 and 422 are the caller's
// request and keep the upstream message; 429 and every 5xx (529 included) are
// retryable; 401 and 403 are the gateway's own credential for this provider,
// so they are provider errors, not a client auth error.
func (c *Connector) systemOneError(status int, msg string) error {
	code, retryable, rejected := "PROVIDER_ERROR", false, false
	switch {
	case status == http.StatusBadRequest, status == http.StatusUnprocessableEntity:
		// No other target can fix the caller's request, so stop failover.
		code, rejected = "INVALID_REQUEST", true
	case status == http.StatusTooManyRequests:
		code, retryable = "THROTTLED", true
	case status >= 500:
		retryable = true
	}
	return &inferencegw.ConnectorError{
		Provider:        c.name,
		Code:            code,
		Retryable:       retryable,
		RequestRejected: rejected,
		HTTPStatus:      status,
		Underlying:      fmt.Errorf("status %d: %s", status, msg),
		ProviderMessage: msg,
	}
}

// upstreamMessage extracts the human-readable message from an error body in
// the shapes seen in the wild ({"error":{"message"}}, {"error":"…"},
// {"message"}, {"detail"}), or returns the trimmed text.
func upstreamMessage(body string) string {
	var e struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
		Detail  json.RawMessage `json:"detail"`
	}
	if json.Unmarshal([]byte(body), &e) == nil {
		var nested struct {
			Message string `json:"message"`
		}
		var text string
		switch {
		case json.Unmarshal(e.Error, &nested) == nil && nested.Message != "":
			return nested.Message
		case json.Unmarshal(e.Error, &text) == nil && text != "":
			return text
		case e.Message != "":
			return e.Message
		case json.Unmarshal(e.Detail, &text) == nil && text != "":
			return text
		}
	}
	return strings.TrimSpace(body)
}

// usageTokens reads usage.input_tokens and usage.output_tokens from a response
// body. A missing or malformed usage counts as zero.
func usageTokens(body []byte) (input, output int64) {
	var r struct {
		Usage struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &r) != nil {
		return 0, 0
	}
	return r.Usage.InputTokens, r.Usage.OutputTokens
}

// withModel returns body with the top-level "model" value replaced by model.
// It splices bytes instead of re-encoding, so state and questions keep their
// exact bytes, key order and whitespace. A body without a model gets one.
func withModel(body []byte, model string) ([]byte, error) {
	enc, err := json.Marshal(model)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errMalformedBody
	}
	type span struct{ start, end int }
	empty := !dec.More()
	var spans []span
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, errMalformedBody
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, errMalformedBody
		}
		if key == "model" {
			end := int(dec.InputOffset())
			spans = append(spans, span{end - len(raw), end})
		}
	}
	if _, err := dec.Token(); err != nil {
		return nil, errMalformedBody
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errMalformedBody
	}

	if len(spans) == 0 {
		// Insert right after the opening brace.
		sep := []byte(",")
		if empty {
			sep = nil
		}
		open := bytes.IndexByte(body, '{') + 1
		ins := append(append([]byte(`"model":`), enc...), sep...)
		return append(append(append([]byte(nil), body[:open]...), ins...), body[open:]...), nil
	}
	var out bytes.Buffer
	prev := 0
	for _, s := range spans {
		out.Write(body[prev:s.start])
		out.Write(enc)
		prev = s.end
	}
	out.Write(body[prev:])
	return out.Bytes(), nil
}
