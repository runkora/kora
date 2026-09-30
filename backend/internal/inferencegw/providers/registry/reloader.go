package registry

import (
	"context"
	"sync"

	"github.com/clear-route/orbit/internal/inferencegw"
	ip "github.com/clear-route/orbit/internal/platform/inferenceproviders"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// providerSink receives each provider set and rediscovers its models.
type providerSink interface {
	SetProviders(map[inferencegw.ProviderName]inferencegw.ProviderConnector, []inferencegw.ProviderInfo)
	ListModels(ctx context.Context) ([]inferencegw.ProviderModel, error)
}

// Reloader loads the mounted InferenceProvider entries, merges the env
// providers underneath, and swaps the result into the sink.
type Reloader struct {
	ConfigDir string
	SecretDir string
	Env       []Built
	Sink      providerSink
	Logger    logging.Logger

	mu sync.Mutex
}

// Reload rebuilds every provider. An unreadable providers.json keeps the
// current set; a mount glitch must not drop every provider.
func (r *Reloader) Reload(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries, err := ip.Load(r.ConfigDir, r.SecretDir)
	if err != nil {
		r.Logger.Error(ctx, "inference providers not reloaded; keeping the current set",
			logging.Field{Key: "error", Value: err.Error()})
		return
	}
	resources := make([]Built, 0, len(entries))
	for _, e := range entries {
		b := FromResource(e, r.Logger)
		if b.Info.BuildError != "" {
			r.Logger.Error(ctx, "inference provider not loaded",
				logging.Field{Key: "provider", Value: e.Name},
				logging.Field{Key: "error", Value: b.Info.BuildError})
		}
		resources = append(resources, b)
	}
	connectors, infos := Merge(r.Env, resources)
	r.Sink.SetProviders(connectors, infos)
	r.Logger.Info(ctx, "inference providers loaded", logging.Field{Key: "providers", Value: len(connectors)})

	go func() {
		if _, err := r.Sink.ListModels(context.WithoutCancel(ctx)); err != nil {
			r.Logger.Warn(ctx, "provider model discovery failed", logging.Field{Key: "error", Value: err.Error()})
		}
	}()
}

// Watch reloads on every change to the mounted directories until ctx ends.
func (r *Reloader) Watch(ctx context.Context) {
	ip.Watch(ctx, r.Logger, []string{r.ConfigDir, r.SecretDir}, r.Reload)
}
