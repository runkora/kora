package agents

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	"github.com/runkora/kora/backend/internal/agents/sandbox/folder"
	"github.com/runkora/kora/backend/internal/agents/manifestpatch"
)

// poolName names each tenant's Deployment and ServiceAccount of unbound
// session-manager pods (docs/adr/2026-11-22-agent-cold-tier.md).
const poolName = "session-manager-pool"

// poolLabel marks an unbound pool pod. The pool ClusterSPIFFEID gives such a
// pod a pool SPIFFE ID, and removing the label releases it from the ReplicaSet.
const poolLabel = "runkora.io/pool"

// poolPodLabels are the labels of an unbound pool pod. The agent egress
// lockdown, the sidecar injection and the SPIRE entry select them.
func poolPodLabels(tenantID string) map[string]string {
	return map[string]string{
		poolLabel:                          "true",
		"runkora.io/tenant":                tenantID,
		folder.ManagedAgentLabel:           "true",
		"spiffe.io/spire-managed-identity": "true",
		"runkora.io/identity-proxy":        "enabled",
	}
}

// buildTenantPool renders the tenant's pool Deployment from the agent pod
// template with the StatefulSet patches applied. A pool pod owns its workspace
// claim as a generic ephemeral volume, and runs as the pool ServiceAccount.
func (p *K8sProvisioner) buildTenantPool(tenantID, namespace string) (appsv1.Deployment, error) {
	ss, err := p.applyStatefulSetPatches(p.buildSessionManagerStatefulSet(AgentType{Slug: poolName, TenantID: tenantID}, namespace, nil))
	if err != nil {
		return appsv1.Deployment{}, err
	}
	template := ss.Spec.Template
	template.Labels = poolPodLabels(tenantID)
	template.Spec.ServiceAccountName = poolName
	for _, claim := range ss.Spec.VolumeClaimTemplates {
		template.Spec.Volumes = append(template.Spec.Volumes, corev1.Volume{
			Name: claim.Name,
			VolumeSource: corev1.VolumeSource{Ephemeral: &corev1.EphemeralVolumeSource{
				VolumeClaimTemplate: &corev1.PersistentVolumeClaimTemplate{Spec: claim.Spec},
			}},
		})
	}
	replicas := p.cfg.PoolSize
	labels := map[string]string{"runkora.io/tenant": tenantID}
	dep := appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: poolName, Namespace: namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{poolLabel: "true"}},
			Template: template,
		},
	}
	dep, err = manifestpatch.Apply(dep, "Deployment", p.cfg.Patches)
	if err != nil {
		return appsv1.Deployment{}, fmt.Errorf("apply Deployment patches: %w", err)
	}
	return dep, nil
}

// ensureTenantPool creates the tenant's pool ServiceAccount and Deployment. On
// update it replaces the Deployment's pod template and replicas with the rendered ones.
func (p *K8sProvisioner) ensureTenantPool(ctx context.Context, tenantID, namespace string, owner metav1.OwnerReference, update bool) error {
	if err := p.ensureServiceAccount(ctx, poolName, namespace, map[string]string{"runkora.io/tenant": tenantID}, owner, update); err != nil {
		return fmt.Errorf("ensure pool serviceaccount: %w", err)
	}
	dep, err := p.buildTenantPool(tenantID, namespace)
	if err != nil {
		return err
	}
	applyOwnerRef(&dep.ObjectMeta, owner)
	deployments := p.client.AppsV1().Deployments(namespace)
	_, err = deployments.Create(ctx, &dep, metav1.CreateOptions{})
	if err == nil || !apierrors.IsAlreadyExists(err) {
		return err
	}
	if !update {
		return nil
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, err := deployments.Get(ctx, poolName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get pool deployment: %w", err)
		}
		existing.Spec.Template = dep.Spec.Template
		existing.Spec.Replicas = dep.Spec.Replicas
		applyOwnerRef(&existing.ObjectMeta, owner)
		_, err = deployments.Update(ctx, existing, metav1.UpdateOptions{})
		return err
	})
}
