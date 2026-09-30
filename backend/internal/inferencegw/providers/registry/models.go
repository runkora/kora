package registry

import (
	"context"
	"slices"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/anthropicapi"
	"github.com/clear-route/orbit/internal/inferencegw/providers/openaicompat"
	ip "github.com/clear-route/orbit/internal/platform/inferenceproviders"
)

// modelListConnector offers only the models a resource lists in spec.models.
// When discovery fails or finds none of them, the list itself is the catalog;
// some compatible servers have no /models endpoint.
type modelListConnector struct {
	inferencegw.ProviderConnector
	typ    ip.Type
	models []string
}

func withModelList(conn inferencegw.ProviderConnector, typ ip.Type, models []string) inferencegw.ProviderConnector {
	if len(models) == 0 {
		return conn
	}
	return &modelListConnector{ProviderConnector: conn, typ: typ, models: models}
}

// ListModels filters discovery to the listed models.
func (c *modelListConnector) ListModels(ctx context.Context) ([]inferencegw.ProviderModel, error) {
	discovered, _ := c.ProviderConnector.ListModels(ctx)
	var kept []inferencegw.ProviderModel
	for _, m := range discovered {
		if slices.Contains(c.models, m.ModelID) || slices.ContainsFunc(m.Aliases, func(a string) bool {
			return slices.Contains(c.models, a)
		}) {
			kept = append(kept, m)
		}
	}
	if len(kept) > 0 {
		return kept, nil
	}
	out := make([]inferencegw.ProviderModel, 0, len(c.models))
	for _, id := range c.models {
		out = append(out, c.describe(id))
	}
	return out, nil
}

func (c *modelListConnector) describe(id string) inferencegw.ProviderModel {
	if c.typ == ip.TypeAnthropic {
		return anthropicapi.ModelFromID(c.Name(), id)
	}
	return openaicompat.ModelFromID(c.Name(), id)
}
