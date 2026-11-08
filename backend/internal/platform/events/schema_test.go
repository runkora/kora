package events

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/runkora/kora/backend/internal/platform/token"
)

// Every catalog type has a payload struct, a description and a schema whose
// enum tags all name a known set.
func TestCatalog_EveryTypeHasASchema(t *testing.T) {
	for _, typ := range Types() {
		if catalog[typ].payload == nil || typ.Description() == "" {
			t.Errorf("%s: no payload struct or description", typ)
			continue
		}
		s, ok := TypeSchema(typ)
		if !ok {
			t.Fatalf("%s: no schema", typ)
		}
		if _, err := json.Marshal(s); err != nil {
			t.Errorf("%s: schema does not marshal: %v", typ, err)
		}
		if err := ValidatePayload(typ, ZeroPayload(typ)); err != nil && !errors.Is(err, errEnumValue) {
			t.Errorf("%s: zero payload: %v", typ, err)
		}
	}
}

func TestTypeSchema_RunFailed(t *testing.T) {
	s, ok := TypeSchema(RunFailed)
	if !ok {
		t.Fatal("no schema")
	}
	if s["$schema"] != SchemaBaseDraft || s["title"] != string(RunFailed) || s["description"] != "A run failed." {
		t.Errorf("header = %v %v %v", s["$schema"], s["title"], s["description"])
	}
	props := s["properties"].(Schema)
	if props["type"].(Schema)["const"] != string(RunFailed) {
		t.Errorf("type = %v, want a const", props["type"])
	}
	entity := props["data"].(Schema)["properties"].(Schema)["entity"].(Schema)
	state := entity["properties"].(Schema)["state"].(Schema)
	if state["description"] != "The run state after the change." || !slices.Contains(state["enum"].([]any), any("failed")) {
		t.Errorf("state = %v", state)
	}
	if !slices.Contains(entity["required"].([]string), "state") || slices.Contains(entity["required"].([]string), "error") {
		t.Errorf("required = %v", entity["required"])
	}
	changed := props["data"].(Schema)["properties"].(Schema)["changed"].(Schema)["items"].(Schema)["enum"].([]string)
	if !slices.Contains(changed, "deleted_at") {
		t.Errorf("changed enum = %v", changed)
	}
}

// changed may name a field the payload does not carry.
func TestChangeable_HiddenFields(t *testing.T) {
	for typ, name := range map[Type]string{AgentUpdated: "system_prompt", TriggerUpdated: "prompt_template", SourceUpdated: "config"} {
		if !slices.Contains(Changeable(typ), name) {
			t.Errorf("%s: changed enum lacks %q", typ, name)
		}
	}
}

func TestFields(t *testing.T) {
	fields, ok := Fields(BudgetThresholdCrossed)
	if !ok {
		t.Fatal("no fields")
	}
	byPath := map[string]Field{}
	for _, f := range fields {
		byPath[f.Path] = f
	}
	if f := byPath["data.entity.level"]; f.Type != "integer" || !slices.Equal(f.Enum, []string{"50", "80", "100"}) {
		t.Errorf("level = %+v", f)
	}
	if f := byPath["koraprincipaltype"]; !slices.Equal(f.Enum, Enum("event_principal_type")) {
		t.Errorf("koraprincipaltype = %+v", f)
	}
	for _, p := range []string{"koraagent", "data.entity", "data.entity.scope", "data.changed", "type"} {
		if _, ok := byPath[p]; !ok {
			t.Errorf("missing %s", p)
		}
	}
	if _, ok := Fields("nope.v1"); ok {
		t.Error("fields of an unknown type")
	}
}

func TestValidatePayload(t *testing.T) {
	ok := RunPayload{ID: "r", AgentID: "a", Agent: "triage", State: "failed", CreatedBy: "u", Error: RunErrorAgentError}
	if err := ValidatePayload(RunFailed, ok); err != nil {
		t.Errorf("valid payload: %v", err)
	}
	bad := ok
	bad.State = "exploded"
	if err := ValidatePayload(RunFailed, bad); err == nil {
		t.Error("a state outside the enum passed")
	}
	if err := ValidatePayload(RunFailed, map[string]any{"id": "r", "secret": "x"}); err == nil {
		t.Error("a field outside the schema passed")
	}
}

// Publish refuses an entity that is not the type's payload struct.
func TestNewEnvelope_RefusesWrongPayload(t *testing.T) {
	for _, entity := range []any{map[string]string{"state": "failed"}, &RunPayload{}, AgentPayload{}, nil} {
		e := Event{Type: RunFailed, Tenant: "t", Subject: "runs/1", RunID: uuid.NewString(), Entity: entity}
		if _, err := newEnvelope(t.Context(), uuid.New(), e, time.Now()); !errors.Is(err, ErrPayloadType) {
			t.Errorf("%T: err = %v, want ErrPayloadType", entity, err)
		}
	}
}

// The enums that mirror token vocabularies hold every value.
func TestEnums_TokenVocabularies(t *testing.T) {
	principals := []string{string(token.PrincipalTypeHuman), string(token.PrincipalTypeAgent), string(token.PrincipalTypeService),
		string(token.PrincipalTypeSystem), string(token.PrincipalTypePAT)}
	if !slices.Equal(Enum("token_principal_type"), principals) {
		t.Errorf("token_principal_type = %v, want %v", Enum("token_principal_type"), principals)
	}
	sources := []string{string(token.IdentitySourceFederated), string(token.IdentitySourceAttested), string(token.IdentitySourcePlatform)}
	if !slices.Equal(Enum("identity_source"), sources) {
		t.Errorf("identity_source = %v, want %v", Enum("identity_source"), sources)
	}
}
