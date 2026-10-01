package registry

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clear-route/orbit/internal/inferencegw"
	ip "github.com/clear-route/orbit/internal/inferencegw/providers/v1alpha1"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// conditionLoaded reports whether the gateway loaded the resource.
const conditionLoaded = "Loaded"

// reloadDebounce coalesces a burst of watch events into one rebuild.
const reloadDebounce = 200 * time.Millisecond

// discoveryTimeout bounds one model discovery across every provider.
const discoveryTimeout = 2 * time.Minute

// providerSink receives each provider set and rediscovers its models.
type providerSink interface {
	SetProviders(map[inferencegw.ProviderName]inferencegw.ProviderConnector, []inferencegw.ProviderInfo)
	ListModels(ctx context.Context) ([]inferencegw.ProviderModel, error)
}

// statusPatcher writes InferenceProvider status.
type statusPatcher interface {
	Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error
}

// informerSource hands out the informers the watcher subscribes to.
type informerSource interface {
	GetInformer(ctx context.Context, obj client.Object, opts ...cache.InformerGetOption) (cache.Informer, error)
}

// Watcher keeps the gateway's providers in step with the InferenceProvider
// resources in one namespace and the labeled Secrets they reference.
type Watcher struct {
	reader    client.Reader
	status    statusPatcher
	informers informerSource
	namespace string
	logger    logging.Logger
}

// NewWatcher starts a cache of InferenceProviders and of Secrets labeled
// ip.SecretLabel in namespace, and waits for it to sync.
func NewWatcher(ctx context.Context, cfg *rest.Config, namespace string, logger logging.Logger) (*Watcher, error) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("register core types: %w", err)
	}
	if err := ip.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("register InferenceProvider: %w", err)
	}
	c, err := cache.New(cfg, cache.Options{
		Scheme:            scheme,
		DefaultNamespaces: map[string]cache.Config{namespace: {}},
		ByObject: map[client.Object]cache.ByObject{
			&corev1.Secret{}: {Label: labels.SelectorFromSet(labels.Set{ip.SecretLabel: "true"})},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create provider cache: %w", err)
	}
	cl, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("create provider status client: %w", err)
	}
	w := &Watcher{reader: c, status: cl.Status(), informers: c, namespace: namespace, logger: logger}
	// Registering the informers before the cache starts makes the sync wait cover them.
	for _, obj := range []client.Object{&ip.InferenceProvider{}, &corev1.Secret{}} {
		if _, err := c.GetInformer(ctx, obj); err != nil {
			return nil, fmt.Errorf("watch %T: %w", obj, err)
		}
	}
	go func() {
		if err := c.Start(ctx); err != nil {
			logger.Error(ctx, "provider cache stopped", logging.Field{Key: "error", Value: err.Error()})
		}
	}()
	if !c.WaitForCacheSync(ctx) {
		return nil, fmt.Errorf("provider cache did not sync")
	}
	return w, nil
}

// Load builds a connector for every resource and records each outcome on its
// status. A resource that fails to build is described without a connector.
func (w *Watcher) Load(ctx context.Context) (map[inferencegw.ProviderName]inferencegw.ProviderConnector, []inferencegw.ProviderInfo) {
	connectors := map[inferencegw.ProviderName]inferencegw.ProviderConnector{}
	var infos []inferencegw.ProviderInfo
	list := &ip.InferenceProviderList{}
	if err := w.reader.List(ctx, list, client.InNamespace(w.namespace)); err != nil {
		w.logger.Error(ctx, "list InferenceProviders", logging.Field{Key: "error", Value: err.Error()})
		return connectors, nil
	}
	for i := range list.Items {
		p := &list.Items[i]
		conn, info, err := buildConnector(ctx, p, w.secret, w.logger)
		infos = append(infos, info)
		if conn != nil {
			connectors[info.Name] = conn
		}
		w.writeStatus(ctx, p, err)
	}
	return connectors, infos
}

// Run reloads the providers into sink on every resource or Secret change
// until ctx ends.
func (w *Watcher) Run(ctx context.Context, sink providerSink) error {
	changed := make(chan struct{}, 1)
	signal := func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	}
	handler := toolscache.ResourceEventHandlerFuncs{
		AddFunc:    func(any) { signal() },
		DeleteFunc: func(any) { signal() },
		UpdateFunc: func(oldObj, newObj any) {
			// A status write by this watcher changes no generation and needs no reload.
			if o, ok := oldObj.(*ip.InferenceProvider); ok && o.Generation == newObj.(*ip.InferenceProvider).Generation {
				return
			}
			signal()
		},
	}
	for _, obj := range []client.Object{&ip.InferenceProvider{}, &corev1.Secret{}} {
		inf, err := w.informers.GetInformer(ctx, obj)
		if err != nil {
			return fmt.Errorf("watch %T: %w", obj, err)
		}
		if _, err := inf.AddEventHandler(handler); err != nil {
			return fmt.Errorf("handle %T events: %w", obj, err)
		}
	}
	cancelDiscovery := func() {}
	defer func() { cancelDiscovery() }()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-changed:
			time.Sleep(reloadDebounce)
			select {
			case <-changed:
			default:
			}
			connectors, infos := w.Load(ctx)
			sink.SetProviders(connectors, infos)
			w.logger.Info(ctx, "inference providers loaded", logging.Field{Key: "providers", Value: len(connectors)})
			// A newer set supersedes the previous discovery; the sink also drops
			// results from a replaced set.
			cancelDiscovery()
			discoverCtx, cancel := context.WithTimeout(ctx, discoveryTimeout)
			cancelDiscovery = cancel
			go func() {
				if _, err := sink.ListModels(discoverCtx); err != nil {
					w.logger.Warn(ctx, "provider model discovery failed", logging.Field{Key: "error", Value: err.Error()})
				}
			}()
		}
	}
}

// secret reads one key of a labeled Secret in the watched namespace.
func (w *Watcher) secret(ctx context.Context, name, key string) (string, error) {
	s := &corev1.Secret{}
	err := w.reader.Get(ctx, client.ObjectKey{Namespace: w.namespace, Name: name}, s)
	if apierrors.IsNotFound(err) {
		return "", buildErr(reasonSecretNotFound, "Secret %q not found in %s with label %s=true", name, w.namespace, ip.SecretLabel)
	}
	if err != nil {
		return "", fmt.Errorf("get Secret %q: %w", name, err)
	}
	v, ok := s.Data[key]
	if !ok || len(v) == 0 {
		return "", buildErr(reasonSecretKeyNotFound, "Secret %q has no key %q", name, key)
	}
	return string(v), nil
}

// writeStatus records the build outcome. It writes only a change. Every
// gateway replica computes the same status and calls it.
func (w *Watcher) writeStatus(ctx context.Context, p *ip.InferenceProvider, loadErr error) {
	next := p.DeepCopy()
	cond := metav1.Condition{Type: conditionLoaded, Status: metav1.ConditionTrue, Reason: reasonLoaded,
		Message: "loaded by the inference gateway", ObservedGeneration: p.Generation}
	next.Status.Phase = ip.PhaseReady
	var be *buildError
	if errors.As(loadErr, &be) {
		cond.Status, cond.Reason, cond.Message = metav1.ConditionFalse, be.Reason, be.Message
		next.Status.Phase = ip.PhaseInvalid
	}
	next.Status.ProviderName = p.ProviderName()
	next.Status.ObservedGeneration = p.Generation
	meta.SetStatusCondition(&next.Status.Conditions, cond)
	if statusEqual(p.Status, next.Status) {
		return
	}
	if err := w.status.Patch(ctx, next, client.MergeFrom(p)); err != nil {
		w.logger.Warn(ctx, "patch InferenceProvider status",
			logging.Field{Key: "inferenceprovider", Value: p.Name},
			logging.Field{Key: "error", Value: err.Error()})
	}
}

func statusEqual(a, b ip.Status) bool {
	if a.Phase != b.Phase || a.ProviderName != b.ProviderName || a.ObservedGeneration != b.ObservedGeneration {
		return false
	}
	ca, cb := meta.FindStatusCondition(a.Conditions, conditionLoaded), meta.FindStatusCondition(b.Conditions, conditionLoaded)
	return ca != nil && cb != nil && ca.Status == cb.Status && ca.Reason == cb.Reason && ca.Message == cb.Message
}
