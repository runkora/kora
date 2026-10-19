package registry

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/runkora/kora/backend/internal/inferencegw"
	ip "github.com/runkora/kora/backend/internal/platform/inferenceproviders/v1alpha1"
)

// stubInformer records the handler Run registers. Only AddEventHandler is called.
type stubInformer struct {
	cache.Informer
	mu      sync.Mutex
	handler toolscache.ResourceEventHandler
}

func (s *stubInformer) AddEventHandler(h toolscache.ResourceEventHandler) (toolscache.ResourceEventHandlerRegistration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = h
	return nil, nil
}

func (s *stubInformer) get() toolscache.ResourceEventHandler {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handler
}

// stubInformers hands out one stub per watched kind.
type stubInformers struct{ providers, secrets *stubInformer }

func (s stubInformers) GetInformer(_ context.Context, obj client.Object, _ ...cache.InformerGetOption) (cache.Informer, error) {
	if _, ok := obj.(*corev1.Secret); ok {
		return s.secrets, nil
	}
	return s.providers, nil
}

// countingSink counts provider sets.
type countingSink struct{ sets atomic.Int32 }

func (c *countingSink) SetProviders(map[inferencegw.ProviderName]inferencegw.ProviderConnector, []inferencegw.ProviderInfo) {
	c.sets.Add(1)
}

func (c *countingSink) ListModels(context.Context) ([]inferencegw.ProviderModel, error) {
	return nil, nil
}

func TestRunDebouncesAndSkipsStatusWrites(t *testing.T) {
	w, _ := newWatcher(t)
	informers := stubInformers{providers: &stubInformer{}, secrets: &stubInformer{}}
	w.informers = informers
	sink := &countingSink{}
	ctx := t.Context()
	go func() { _ = w.Run(ctx, sink) }()

	deadline := time.Now().Add(2 * time.Second)
	for (informers.providers.get() == nil || informers.secrets.get() == nil) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	providers, secrets := informers.providers.get(), informers.secrets.get()
	settle := func() { time.Sleep(3 * reloadDebounce) }

	gen := func(g int64) *ip.InferenceProvider {
		return &ip.InferenceProvider{ObjectMeta: metav1.ObjectMeta{Name: "p", Generation: g}}
	}
	providers.OnAdd(gen(1), false)
	providers.OnUpdate(gen(1), gen(2))
	secrets.OnAdd(&corev1.Secret{}, false)
	settle()
	if got := sink.sets.Load(); got != 1 {
		t.Fatalf("a burst of events gave %d reloads, want 1", got)
	}

	providers.OnUpdate(gen(2), gen(2))
	settle()
	if got := sink.sets.Load(); got != 1 {
		t.Errorf("a status-only update reloaded; reloads = %d", got)
	}

	secrets.OnUpdate(&corev1.Secret{}, &corev1.Secret{})
	settle()
	providers.OnDelete(gen(2))
	settle()
	if got := sink.sets.Load(); got != 3 {
		t.Errorf("a Secret update and a delete gave %d reloads in total, want 3", got)
	}
}

// blockingSink holds each discovery open until its context ends and counts the
// ones that were canceled.
type blockingSink struct {
	countingSink
	started  atomic.Int32
	canceled atomic.Int32
}

func (b *blockingSink) ListModels(ctx context.Context) ([]inferencegw.ProviderModel, error) {
	b.started.Add(1)
	<-ctx.Done()
	b.canceled.Add(1)
	return nil, ctx.Err()
}

func TestRunCancelsThePreviousDiscovery(t *testing.T) {
	w, _ := newWatcher(t)
	informers := stubInformers{providers: &stubInformer{}, secrets: &stubInformer{}}
	w.informers = informers
	sink := &blockingSink{}
	ctx := t.Context()
	go func() { _ = w.Run(ctx, sink) }()

	deadline := time.Now().Add(2 * time.Second)
	for informers.secrets.get() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	secrets := informers.secrets.get()
	secrets.OnAdd(&corev1.Secret{}, false)
	time.Sleep(3 * reloadDebounce)
	secrets.OnAdd(&corev1.Secret{}, false)
	time.Sleep(3 * reloadDebounce)

	if sink.started.Load() != 2 || sink.canceled.Load() != 1 {
		t.Errorf("started %d, canceled %d; want the first discovery canceled by the second reload",
			sink.started.Load(), sink.canceled.Load())
	}
}
