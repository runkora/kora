package agents

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

//go:fix inline

func TestValidateSubagentCap(t *testing.T) {
	tests := []struct {
		name string
		in   *int
		ok   bool
	}{
		{"unset", nil, true},
		{"default", new(0), true},
		{"one", new(1), true},
		{"max", new(maxSubagentCap), true},
		{"negative", new(-1), false},
		{"over max", new(maxSubagentCap + 1), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSubagentCap(tt.in)
			if tt.ok != (err == nil) {
				t.Fatalf("validateSubagentCap(%v) = %v, want ok=%v", tt.in, err, tt.ok)
			}
			if err != nil && !errors.Is(err, ErrInvalidSubagentPolicy) {
				t.Errorf("error %v is not ErrInvalidSubagentPolicy", err)
			}
		})
	}
}

func TestSubagentLimits(t *testing.T) {
	tests := []struct {
		name       string
		at         AgentType
		cap, depth int
	}{
		{"defaults", AgentType{NestedSubagents: true}, 0, 0},
		{"capped", AgentType{MaxConcurrentSubagents: new(1), NestedSubagents: true}, 1, 0},
		{"no nesting", AgentType{NestedSubagents: false}, 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, d := tt.at.SubagentLimits()
			if c != tt.cap || d != tt.depth {
				t.Errorf("SubagentLimits() = %d, %d; want %d, %d", c, d, tt.cap, tt.depth)
			}
		})
	}
}

func TestApplyAgentTypeUpdates_SubagentPolicy(t *testing.T) {
	at := &AgentType{MaxConcurrentSubagents: new(3), NestedSubagents: true}

	applyAgentTypeUpdates(at, UpdateAgentTypeRequest{})
	if at.MaxConcurrentSubagents == nil || *at.MaxConcurrentSubagents != 3 || !at.NestedSubagents {
		t.Fatalf("omitted fields changed the policy: %+v", at)
	}

	no := false
	applyAgentTypeUpdates(at, UpdateAgentTypeRequest{MaxConcurrentSubagents: new(0), NestedSubagents: &no})
	if at.MaxConcurrentSubagents != nil || at.NestedSubagents {
		t.Errorf("0 must restore the default and false must disable nesting: cap %v nested %v", at.MaxConcurrentSubagents, at.NestedSubagents)
	}
}

func TestCreateAgentType_SubagentPolicy(t *testing.T) {
	no := false
	tests := []struct {
		name       string
		req        CreateAgentTypeRequest
		wantCap    *int
		wantNested bool
		wantErr    error
	}{
		{"defaults", CreateAgentTypeRequest{}, nil, true, nil},
		{"capped without nesting", CreateAgentTypeRequest{MaxConcurrentSubagents: new(1), NestedSubagents: &no}, new(1), false, nil},
		{"zero is the default", CreateAgentTypeRequest{MaxConcurrentSubagents: new(0)}, nil, true, nil},
		{"out of range", CreateAgentTypeRequest{MaxConcurrentSubagents: new(-2)}, nil, false, ErrInvalidSubagentPolicy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _, _ := newTestService(t)
			ctx := context.Background()
			tt.req.TenantID, tt.req.Name, tt.req.Slug = "t-1", "Bot", "bot"
			at, err := svc.CreateAgentType(ctx, tt.req)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateAgentType: %v", err)
			}
			stored, err := repo.GetAgentType(ctx, at.ID)
			if err != nil {
				t.Fatalf("get stored: %v", err)
			}
			if fmt.Sprint(deref(stored.MaxConcurrentSubagents)) != fmt.Sprint(deref(tt.wantCap)) || stored.NestedSubagents != tt.wantNested {
				t.Errorf("stored cap %v nested %v, want %v %v", deref(stored.MaxConcurrentSubagents), stored.NestedSubagents, deref(tt.wantCap), tt.wantNested)
			}
		})
	}
}

func deref(n *int) any {
	if n == nil {
		return nil
	}
	return *n
}
