package mount

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// Pod selectors for the two sides of a mount.
const (
	sandboxSelector   = "orbit.io/role=sandbox"
	workspaceSelector = "orbit.io/managed-agent=true"
)

// Config holds the node agent's settings.
type Config struct {
	// Node is this node's name.
	Node string
	// KubeletDir is where the node's kubelet directory is mounted in the agent's container.
	KubeletDir string
	// Resync is how often the agent reconciles without a pod event.
	Resync time.Duration
	// Retry is how soon the agent reconciles again after a failure.
	Retry time.Duration
}

// NewAgent returns an agent that serves exports and mounts on this node.
func NewAgent(cfg Config, kube kubernetes.Interface, exports exporter, mounts mounter, logger logging.Logger) *Agent {
	return &Agent{
		cfg:     cfg,
		kube:    kube,
		exports: exports,
		mounts:  mounts,
		logger:  logger,
	}
}

// Run reconciles on every pod change and on the resync interval until ctx ends.
func (a *Agent) Run(ctx context.Context) error {
	sandboxes := podInformer(a.kube, sandboxSelector, a.cfg.Resync)
	workspaces := podInformer(a.kube, workspaceSelector, a.cfg.Resync)
	wake := make(chan struct{}, 1)
	notify := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(any) { poke(wake) },
		UpdateFunc: func(any, any) { poke(wake) },
		DeleteFunc: func(any) { poke(wake) },
	}
	for _, inf := range []informers.SharedInformerFactory{sandboxes, workspaces} {
		if _, err := inf.Core().V1().Pods().Informer().AddEventHandler(notify); err != nil {
			return err
		}
		inf.Start(ctx.Done())
		inf.WaitForCacheSync(ctx.Done())
	}
	sandboxLister := sandboxes.Core().V1().Pods().Lister()
	workspaceLister := workspaces.Core().V1().Pods().Lister()
	workspace := func(namespace, name string) *corev1.Pod {
		pod, err := workspaceLister.Pods(namespace).Get(name)
		if err != nil {
			return nil
		}
		return pod
	}

	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-wake:
		case <-timer.C:
		}
		next := a.cfg.Resync
		pods, err := sandboxLister.List(labels.Everything())
		if err == nil {
			err = a.Reconcile(ctx, pods, workspace)
		}
		if err != nil {
			a.logger.Warn(ctx, "Sandbox folder reconcile incomplete", logging.Field{Key: "error", Value: err.Error()})
			next = a.cfg.Retry
		}
		timer.Reset(next)
	}
}

// podInformer watches the pods matching selector in every namespace.
func podInformer(kube kubernetes.Interface, selector string, resync time.Duration) informers.SharedInformerFactory {
	return informers.NewSharedInformerFactoryWithOptions(kube, resync,
		informers.WithTweakListOptions(func(o *metav1.ListOptions) { o.LabelSelector = selector }))
}

// poke wakes the loop once, however many events arrive before it runs.
func poke(wake chan struct{}) {
	select {
	case wake <- struct{}{}:
	default:
	}
}
