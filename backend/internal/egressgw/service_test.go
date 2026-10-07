package egressgw

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// heldScopes allows exactly the listed "<path>:<action>" scopes.
type heldScopes []string

func (h heldScopes) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	scope := strings.Join(in.PathSegments, ":") + ":" + in.Action
	return policy.Result{Allow: slices.Contains(h, scope)}, nil
}

// failingOn errors for one scope and allows every other.
type failingOn string

func (f failingOn) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	if strings.Join(in.PathSegments, ":")+":"+in.Action == string(f) {
		return policy.Result{}, errors.New("evaluator down")
	}
	return policy.Result{Allow: true}, nil
}

func TestEvaluatePolicy_AlsoRequiredEvaluatorErrorFailsClosed(t *testing.T) {
	cls := credentials.Classification{Action: "pulls:read", AlsoRequired: []string{"contents:read"}, Qualifier: "orbit"}
	b := NewBroker(failingOn("github:contents:orbit:read"), nil, nil, nil, logging.NewLoggerWithOutput(io.Discard))
	got, err := b.evaluatePolicy(context.Background(), &token.PlatformClaims{}, &credentials.Connection{Name: "github"}, cls, EgressRequest{Host: "api.github.com", Method: "POST"})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v (authorized %v)", err, got)
	}
}

func TestEvaluatePolicy_AlsoRequired(t *testing.T) {
	conn := &credentials.Connection{Name: "github"}
	cls := credentials.Classification{
		Action: "issues:read", Alternates: []string{"pulls:read"},
		AlsoRequired: []string{"contents:read"}, Qualifier: "orbit",
	}
	cases := []struct {
		name string
		held heldScopes
		want []string
	}{
		{"all held", heldScopes{"github:issues:orbit:read", "github:contents:orbit:read"}, []string{"issues:read", "contents:read"}},
		{"alternate and required held", heldScopes{"github:pulls:orbit:read", "github:contents:orbit:read"}, []string{"pulls:read", "contents:read"}},
		{"required missing", heldScopes{"github:issues:orbit:read", "github:pulls:orbit:read"}, nil},
		{"only required held", heldScopes{"github:contents:orbit:read"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBroker(tc.held, nil, nil, nil, logging.NewLoggerWithOutput(io.Discard))
			got, err := b.evaluatePolicy(context.Background(), &token.PlatformClaims{}, conn, cls, EgressRequest{Host: "api.github.com", Method: "POST"})
			if tc.want == nil {
				if !errors.Is(err, ErrForbidden) {
					t.Fatalf("want ErrForbidden, got %v (authorized %v)", err, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("evaluatePolicy: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("authorized = %v, want %v", got, tc.want)
			}
		})
	}
}

// A missing required action is named in the denial, with the scope that
// would allow it.
func TestEvaluatePolicy_AlsoRequiredDenialNamesAction(t *testing.T) {
	cls := credentials.Classification{Action: "pulls:read", AlsoRequired: []string{"contents:read"}, Qualifier: "orbit"}
	b := NewBroker(heldScopes{"github:pulls:orbit:read"}, nil, nil, nil, logging.NewLoggerWithOutput(io.Discard))
	_, err := b.evaluatePolicy(context.Background(), &token.PlatformClaims{}, &credentials.Connection{Name: "github"}, cls, EgressRequest{Host: "api.github.com", Method: "POST"})
	var d *Denial
	if !errors.As(err, &d) {
		t.Fatalf("want a Denial, got %v", err)
	}
	if d.Action != "contents:read" || d.Reason != DenialNotGranted || d.RequiredScope == "" {
		t.Errorf("denial = %+v, want contents:read not granted with a required scope", d)
	}
}
