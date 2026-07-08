package orbitmcp

import (
	"context"
	"errors"
	"net/url"

	"github.com/clear-route/orbit/internal/platform/clients/orbitjson"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerModelTools wires the models domain: the inference catalog exposed by
// the inference gateway — the runtime model list plus full management of virtual
// models, provider models, and pricing overrides. Reads require platform:catalog:read,
// writes platform:catalog:write; the inference gateway enforces the real scope.
func registerModelTools(srv *sdk.Server, b Backends, jc *orbitjson.Client) {
	base := b.Inference + "/v1"
	catalog := base + "/catalog"

	addTool(srv, "models.list",
		"List the inference models available through Orbit at runtime, with provider and capability metadata.",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), base+"/models", nil))
		})

	// ── virtual models (tenant-facing model aliases with routing) ────────────────

	addTool(srv, "models.virtual.list",
		"List the virtual models (tenant-facing model aliases with routing + pricing) in the catalog.",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), catalog+"/virtual-models", nil))
		})

	addTool(srv, "models.virtual.get",
		"Get a single virtual model by id, including its routes and effective pricing.",
		func(ctx context.Context, a virtualModelIDArgs) (*sdk.CallToolResult, error) {
			if a.ID == "" {
				return toolError(errors.New("id is required")), nil
			}
			return jsonResult(jc.Get(ctx, callerToken(ctx), catalog+"/virtual-models/"+url.PathEscape(a.ID), nil))
		})

	addTool(srv, "models.virtual.create",
		"Create a virtual model. A virtual model routes a tenant-facing alias to one or more provider models "+
			"(routes), with a routing strategy. Call models.providers.list for valid provider/model_id pairs.",
		func(ctx context.Context, a virtualModelArgs) (*sdk.CallToolResult, error) {
			if a.Name == "" {
				return toolError(errors.New("name is required")), nil
			}
			return jsonResult(jc.Post(ctx, callerToken(ctx), catalog+"/virtual-models", a))
		})

	addTool(srv, "models.virtual.update",
		"Update a virtual model in place (replaces its fields and routes). Fetch it first with models.virtual.get.",
		func(ctx context.Context, a updateVirtualModelArgs) (*sdk.CallToolResult, error) {
			if a.ID == "" {
				return toolError(errors.New("id is required")), nil
			}
			body := virtualModelArgs{
				Family: a.Family, Name: a.Name, DisplayName: a.DisplayName, Description: a.Description,
				Strategy: a.Strategy, Enabled: a.Enabled, Routes: a.Routes,
			}
			return jsonResult(jc.Put(ctx, callerToken(ctx), catalog+"/virtual-models/"+url.PathEscape(a.ID), body))
		})

	addTool(srv, "models.virtual.delete",
		"Delete a virtual model by id.",
		func(ctx context.Context, a virtualModelIDArgs) (*sdk.CallToolResult, error) {
			if a.ID == "" {
				return toolError(errors.New("id is required")), nil
			}
			return jsonResult(jc.Delete(ctx, callerToken(ctx), catalog+"/virtual-models/"+url.PathEscape(a.ID)))
		})

	registerProviderModelTools(srv, catalog, jc)
	registerPricingTools(srv, catalog, jc)
}

// registerProviderModelTools wires the provider-model catalog (the upstream
// models from Bedrock/Vertex/Azure that virtual models route to).
func registerProviderModelTools(srv *sdk.Server, catalog string, jc *orbitjson.Client) {
	addTool(srv, "models.providers.list",
		"List the provider models (the upstream models from Bedrock/Vertex/Azure that virtual models route to), "+
			"with capabilities and pricing.",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), catalog+"/provider-models", nil))
		})

	addTool(srv, "models.providers.refresh",
		"Refresh the provider-model catalog from the upstream providers (re-syncs the available models).",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Post(ctx, callerToken(ctx), catalog+"/provider-models/refresh", nil))
		})

	addTool(srv, "models.providers.set_enabled",
		"Enable or disable a provider model (whether it can be used as a virtual-model route target).",
		func(ctx context.Context, a setProviderEnabledArgs) (*sdk.CallToolResult, error) {
			if a.Provider == "" || a.ModelID == "" {
				return toolError(errors.New("provider and model_id are required")), nil
			}
			body := map[string]any{"enabled": a.Enabled}
			return jsonResult(jc.Put(ctx, callerToken(ctx),
				catalog+"/provider-models/"+url.PathEscape(a.Provider)+"/"+url.PathEscape(a.ModelID), body))
		})
}

// registerPricingTools wires the per-provider-model pricing overrides.
func registerPricingTools(srv *sdk.Server, catalog string, jc *orbitjson.Client) {
	addTool(srv, "models.pricing.list",
		"List per-provider-model pricing (default rates plus any tenant overrides).",
		func(ctx context.Context, _ noArgs) (*sdk.CallToolResult, error) {
			return jsonResult(jc.Get(ctx, callerToken(ctx), catalog+"/pricing", nil))
		})

	addTool(srv, "models.pricing.set_override",
		"Set a pricing override for a provider model. Supply at least one cost field (per 1M tokens); "+
			"omitted fields keep the default. Overrides feed cost tracking and budgets.",
		func(ctx context.Context, a setPricingOverrideArgs) (*sdk.CallToolResult, error) {
			if a.Provider == "" || a.ModelID == "" {
				return toolError(errors.New("provider and model_id are required")), nil
			}
			body := map[string]any{}
			if a.InputCostPer1M != nil {
				body["input_cost_per_1m"] = *a.InputCostPer1M
			}
			if a.OutputCostPer1M != nil {
				body["output_cost_per_1m"] = *a.OutputCostPer1M
			}
			if a.CacheReadCostPer1M != nil {
				body["cache_read_cost_per_1m"] = *a.CacheReadCostPer1M
			}
			if a.CacheWriteCostPer1M != nil {
				body["cache_write_cost_per_1m"] = *a.CacheWriteCostPer1M
			}
			if len(body) == 0 {
				return toolError(errors.New("at least one cost field is required")), nil
			}
			return jsonResult(jc.Put(ctx, callerToken(ctx),
				catalog+"/pricing/"+url.PathEscape(a.Provider)+"/"+url.PathEscape(a.ModelID)+"/override", body))
		})

	addTool(srv, "models.pricing.clear_override",
		"Remove the pricing override for a provider model, reverting it to the default rates.",
		func(ctx context.Context, a providerModelArgs) (*sdk.CallToolResult, error) {
			if a.Provider == "" || a.ModelID == "" {
				return toolError(errors.New("provider and model_id are required")), nil
			}
			return jsonResult(jc.Delete(ctx, callerToken(ctx),
				catalog+"/pricing/"+url.PathEscape(a.Provider)+"/"+url.PathEscape(a.ModelID)+"/override"))
		})
}

// ── tool argument types ───────────────────────────────────────────────────────

type virtualModelIDArgs struct {
	ID string `json:"id" jsonschema:"the virtual model id"`
}

// routeArg is one routing target of a virtual model (a provider model to route to).
type routeArg struct {
	Provider string `json:"provider" jsonschema:"the provider (e.g. bedrock, vertexai, azure)"`
	ModelID  string `json:"model_id" jsonschema:"the provider model id to route to"`
	Priority int    `json:"priority,omitempty" jsonschema:"routing priority (lower = preferred)"`
	Weight   int    `json:"weight,omitempty" jsonschema:"weight for weighted strategies"`
	Enabled  bool   `json:"enabled,omitempty" jsonschema:"whether this route is enabled"`
}

type virtualModelArgs struct {
	Family      string     `json:"family,omitempty" jsonschema:"model family grouping (e.g. claude)"`
	Name        string     `json:"name" jsonschema:"the virtual model name (the tenant-facing alias)"`
	DisplayName string     `json:"display_name,omitempty" jsonschema:"human-friendly display name"`
	Description string     `json:"description,omitempty" jsonschema:"what this model is for"`
	Strategy    string     `json:"strategy,omitempty" jsonschema:"routing strategy across routes (e.g. priority, weighted)"`
	Enabled     bool       `json:"enabled,omitempty" jsonschema:"whether the virtual model is enabled"`
	Routes      []routeArg `json:"routes,omitempty" jsonschema:"the provider models this alias routes to"`
}

type updateVirtualModelArgs struct {
	ID          string     `json:"id" jsonschema:"the virtual model id to update"`
	Family      string     `json:"family,omitempty" jsonschema:"model family grouping (e.g. claude)"`
	Name        string     `json:"name" jsonschema:"the virtual model name (the tenant-facing alias)"`
	DisplayName string     `json:"display_name,omitempty" jsonschema:"human-friendly display name"`
	Description string     `json:"description,omitempty" jsonschema:"what this model is for"`
	Strategy    string     `json:"strategy,omitempty" jsonschema:"routing strategy across routes"`
	Enabled     bool       `json:"enabled,omitempty" jsonschema:"whether the virtual model is enabled"`
	Routes      []routeArg `json:"routes,omitempty" jsonschema:"the provider models this alias routes to"`
}

type setProviderEnabledArgs struct {
	Provider string `json:"provider" jsonschema:"the provider (bedrock, vertexai, azure)"`
	ModelID  string `json:"model_id" jsonschema:"the provider model id"`
	Enabled  bool   `json:"enabled" jsonschema:"whether to enable the provider model"`
}

type providerModelArgs struct {
	Provider string `json:"provider" jsonschema:"the provider (bedrock, vertexai, azure)"`
	ModelID  string `json:"model_id" jsonschema:"the provider model id"`
}

type setPricingOverrideArgs struct {
	Provider            string   `json:"provider" jsonschema:"the provider (bedrock, vertexai, azure)"`
	ModelID             string   `json:"model_id" jsonschema:"the provider model id"`
	InputCostPer1M      *float64 `json:"input_cost_per_1m,omitempty" jsonschema:"input token cost per 1M tokens (USD)"`
	OutputCostPer1M     *float64 `json:"output_cost_per_1m,omitempty" jsonschema:"output token cost per 1M tokens (USD)"`
	CacheReadCostPer1M  *float64 `json:"cache_read_cost_per_1m,omitempty" jsonschema:"cache-read cost per 1M tokens (USD)"`
	CacheWriteCostPer1M *float64 `json:"cache_write_cost_per_1m,omitempty" jsonschema:"cache-write cost per 1M tokens (USD)"`
}
