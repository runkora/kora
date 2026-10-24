package inferencegw

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// fakeCatalogRepo answers the virtual model calls the authoring tests make.
// Any other call panics on the nil embedded interface.
type fakeCatalogRepo struct {
	CatalogRepository
	vms   []VirtualModel
	err   error
	wrote bool
}

func (f *fakeCatalogRepo) ListVirtualModels(context.Context) ([]VirtualModel, error) {
	return f.vms, nil
}

func (f *fakeCatalogRepo) CreateVirtualModel(context.Context, VirtualModel) error {
	f.wrote = true
	return f.err
}

func (f *fakeCatalogRepo) UpdateVirtualModel(context.Context, VirtualModel) (VirtualModel, error) {
	f.wrote = true
	return VirtualModel{}, f.err
}

func (f *fakeCatalogRepo) DeleteVirtualModel(context.Context, string) error {
	f.wrote = true
	return f.err
}

func newAuthoringController(repo *fakeCatalogRepo, apiAuthoring bool) *CatalogController {
	return &CatalogController{
		store:          NewCatalogStore(repo, nil),
		pricingCatalog: &mockPricingCatalogRepository{},
		apiAuthoring:   apiAuthoring,
		logger:         logging.NewLoggerWithOutput(io.Discard),
	}
}

const validVirtualModelBody = `{"family":"anthropic","name":"sonnet","display_name":"Sonnet","strategy":"priority",
	"routes":[{"provider":"bedrock:prod","model_id":"m","priority":1,"enabled":true}]}`

// virtualModelWrites returns each write handler with a request for it.
func virtualModelWrites(c *CatalogController) map[string]func() *httptest.ResponseRecorder {
	call := func(h http.HandlerFunc, method, body string) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			req := httptest.NewRequest(method, "/virtual-models/vm-1", strings.NewReader(body))
			req.SetPathValue("id", "vm-1")
			w := httptest.NewRecorder()
			h(w, req)
			return w
		}
	}
	return map[string]func() *httptest.ResponseRecorder{
		"create": call(c.handleCreateVirtualModel, http.MethodPost, validVirtualModelBody),
		"update": call(c.handleUpdateVirtualModel, http.MethodPut, validVirtualModelBody),
		"delete": call(c.handleDeleteVirtualModel, http.MethodDelete, ""),
	}
}

func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return body.Code
}

func TestVirtualModelWritesRefusedWhenAuthoringDisabled(t *testing.T) {
	repo := &fakeCatalogRepo{}
	for name, do := range virtualModelWrites(newAuthoringController(repo, false)) {
		w := do()
		if w.Code != http.StatusForbidden || errorCode(t, w) != api.ErrCodeAPIAuthoringDisabled {
			t.Errorf("%s: status = %d, want 403 api_authoring_disabled", name, w.Code)
		}
	}
	if repo.wrote {
		t.Error("a refused write reached the store")
	}
}

func TestVirtualModelWritesToDeclaredRowsConflict(t *testing.T) {
	repo := &fakeCatalogRepo{err: fmt.Errorf("write: %w", ErrVirtualModelManaged)}
	for name, do := range virtualModelWrites(newAuthoringController(repo, true)) {
		w := do()
		if w.Code != http.StatusConflict || errorCode(t, w) != api.ErrCodeManagedByResource {
			t.Errorf("%s: status = %d, want 409 managed_by_resource", name, w.Code)
		}
	}
}

func TestCreateVirtualModelOnTakenKeyConflicts(t *testing.T) {
	repo := &fakeCatalogRepo{err: ErrVirtualModelExists}
	w := virtualModelWrites(newAuthoringController(repo, true))["create"]()
	if w.Code != http.StatusConflict || errorCode(t, w) != api.ErrCodeResourceExists {
		t.Errorf("status = %d, want 409 resource_already_exists", w.Code)
	}
}

func TestListVirtualModelsCarriesSourceAndSwitch(t *testing.T) {
	repo := &fakeCatalogRepo{vms: []VirtualModel{
		{ID: "a", Family: "f", Name: "a", Source: SourceAPI},
		{ID: "b", Family: "f", Name: "b", Source: SourceCRD, ResourceUID: "uid"},
	}}
	for _, enabled := range []bool{true, false} {
		w := httptest.NewRecorder()
		newAuthoringController(repo, enabled).handleListVirtualModels(w, httptest.NewRequest(http.MethodGet, "/virtual-models", nil))
		var body struct {
			Data []struct {
				ID     string `json:"id"`
				Source string `json:"source"`
			} `json:"data"`
			APIAuthoring bool `json:"api_authoring"`
		}
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.APIAuthoring != enabled {
			t.Errorf("api_authoring = %v, want %v", body.APIAuthoring, enabled)
		}
		if len(body.Data) != 2 || body.Data[0].Source != "api" || body.Data[1].Source != "crd" {
			t.Errorf("data = %+v, want sources api and crd", body.Data)
		}
	}
}

func TestValidateVirtualModelRejectsRepeatedRoute(t *testing.T) {
	route := VirtualModelRoute{Provider: "bedrock:prod", ModelID: "m", Priority: 1, Enabled: true}
	vm := VirtualModel{Family: "f", Name: "n", DisplayName: "N", Strategy: VirtualStrategyPriority, Routes: []VirtualModelRoute{route, route}}
	if err := ValidateVirtualModel(vm); err == nil || !strings.Contains(err.Error(), "repeats bedrock:prod/m") {
		t.Errorf("err = %v, want a repeated route error", err)
	}
	vm.Routes = vm.Routes[:1]
	if err := ValidateVirtualModel(vm); err != nil {
		t.Errorf("err = %v, want valid", err)
	}
}
