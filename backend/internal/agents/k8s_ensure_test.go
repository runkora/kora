package agents

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// The budget, Service and ServiceAccount updates recover from a concurrent write, such as the disruption controller's status update.
func TestEnsureUpdatesRetryOnConflict(t *testing.T) {
	at := AgentType{Slug: "coder", TenantID: "acme"}
	ctx := context.Background()
	labels := agentTypeLabels(at)
	one := intstr.FromInt32(1)
	for resource, c := range map[string]struct {
		live  runtime.Object
		write func(p *K8sProvisioner) error
		check func(client *fake.Clientset) error
	}{
		"poddisruptionbudgets": {
			live: &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: scaleTestNS},
				Spec: policyv1.PodDisruptionBudgetSpec{MinAvailable: &one}},
			write: func(p *K8sProvisioner) error {
				return p.ensurePDB(ctx, at.Slug, scaleTestNS, labels, metav1.OwnerReference{}, true)
			},
			check: func(client *fake.Clientset) error {
				got, err := client.PolicyV1().PodDisruptionBudgets(scaleTestNS).Get(ctx, at.Slug, metav1.GetOptions{})
				if err == nil && (got.Spec.MinAvailable != nil || got.Spec.MaxUnavailable == nil) {
					err = errors.New("budget kept minAvailable")
				}
				return err
			},
		},
		"services": {
			live: &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: scaleTestNS}},
			write: func(p *K8sProvisioner) error {
				return p.ensureHeadlessService(ctx, at.Slug, scaleTestNS, labels, metav1.OwnerReference{}, true)
			},
			check: func(client *fake.Clientset) error {
				got, err := client.CoreV1().Services(scaleTestNS).Get(ctx, at.Slug, metav1.GetOptions{})
				if err == nil && len(got.Spec.Ports) == 0 {
					err = errors.New("service has no ports")
				}
				return err
			},
		},
		"serviceaccounts": {
			live: &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: at.Slug, Namespace: scaleTestNS}},
			write: func(p *K8sProvisioner) error {
				return p.ensureServiceAccount(ctx, at.Slug, scaleTestNS, labels, metav1.OwnerReference{}, true)
			},
			check: func(client *fake.Clientset) error {
				got, err := client.CoreV1().ServiceAccounts(scaleTestNS).Get(ctx, at.Slug, metav1.GetOptions{})
				if err == nil && len(got.Labels) == 0 {
					err = errors.New("serviceaccount has no labels")
				}
				return err
			},
		},
	} {
		t.Run(resource, func(t *testing.T) {
			client := fake.NewSimpleClientset(c.live)
			failed := false
			client.PrependReactor("update", resource, func(k8stesting.Action) (bool, runtime.Object, error) {
				if failed {
					return false, nil, nil
				}
				failed = true
				return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: resource}, at.Slug, errors.New("changed"))
			})
			p := podSecurityProvisioner()
			p.cfg.MinReplicas = 1
			p.client = client
			if err := c.write(p); err != nil {
				t.Fatalf("update after one conflict: %v", err)
			}
			if err := c.check(client); err != nil {
				t.Error(err)
			}
		})
	}
}
