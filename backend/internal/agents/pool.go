package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

// poolName names each tenant's Deployment, ServiceAccount and
// AuthorizationPolicy of unbound session-manager pods
// (docs/adr/2026-11-22-agent-cold-tier.md). It is a reserved agent slug.
const poolName = "session-manager-pool"

// poolLabel marks an unbound pool pod. The pool ClusterSPIFFEID gives such a
// pod a pool SPIFFE ID, and removing the label releases it from the ReplicaSet.
const poolLabel = "runkora.io/pool"

// poolTemplateAnnotation carries a hash of the pool pod template on the template itself. A bound pod
// whose hash differs from the pool Deployment's is on an old template, and the scaler rolls it.
const poolTemplateAnnotation = "runkora.io/pool-template"

// agentOnlyPodLabels name the agent a pod serves. A pool pod carries none of them until the bind.
var agentOnlyPodLabels = []string{"app", "spiffe.io/agent", agentTypeLabel}

// buildTenantPool renders the tenant's pool Deployment from the agent pod
// template with the StatefulSet patches applied. A pool pod keeps the
// template's workload and patch labels, carries no agent label, owns its
// workspace claim as a generic ephemeral volume, runs as the pool ServiceAccount
// and starts at the warm session-manager requests. The bind grows it to full size
// (docs/adr/2026-11-22-agent-cold-tier.md, W1).
func (p *K8sProvisioner) buildTenantPool(tenantID, namespace string) (appsv1.Deployment, error) {
	// The stand-in agent's labels let a StatefulSet patch that selects every agent reach the pool.
	standIn := AgentType{Slug: poolName, TenantID: tenantID}
	ss, err := p.applyStatefulSetPatches(p.buildSessionManagerStatefulSet(standIn, namespace, agentTypeLabels(standIn)))
	if err != nil {
		return appsv1.Deployment{}, err
	}
	template := ss.Spec.Template
	// The agent pod labels the template keeps put the pool pod under the agent
	// egress lockdown, the sidecar injection and the SPIRE entry.
	podLabels := maps.Clone(template.Labels)
	for _, k := range agentOnlyPodLabels {
		delete(podLabels, k)
	}
	podLabels[poolLabel] = "true"
	template.Labels = podLabels
	template.Spec.ServiceAccountName = poolName
	for i, c := range template.Spec.Containers {
		if c.Name == sessionManagerContainerName {
			maps.Copy(template.Spec.Containers[i].Resources.Requests, p.warmRequests(fullRequests(&corev1.Pod{Spec: template.Spec}, p.fullRequests())))
		}
	}
	for _, claim := range ss.Spec.VolumeClaimTemplates {
		template.Spec.Volumes = append(template.Spec.Volumes, corev1.Volume{
			Name: claim.Name,
			VolumeSource: corev1.VolumeSource{Ephemeral: &corev1.EphemeralVolumeSource{
				VolumeClaimTemplate: &corev1.PersistentVolumeClaimTemplate{Spec: claim.Spec},
			}},
		})
	}
	hash, err := json.Marshal(template)
	if err != nil {
		return appsv1.Deployment{}, fmt.Errorf("encode pool pod template: %w", err)
	}
	sum := sha256.Sum256(hash)
	if template.Annotations == nil {
		template.Annotations = map[string]string{}
	}
	template.Annotations[poolTemplateAnnotation] = hex.EncodeToString(sum[:8])
	replicas := p.cfg.PoolSize
	labels := map[string]string{"runkora.io/tenant": tenantID, poolLabel: "true"}
	return appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: poolName, Namespace: namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{poolLabel: "true"}},
			Template: template,
		},
	}, nil
}

// ensureTenantPool creates the tenant's pool ServiceAccount, Deployment and
// AuthorizationPolicy. On update it replaces the Deployment's labels, pod
// template and replicas with the rendered ones.
func (p *K8sProvisioner) ensureTenantPool(ctx context.Context, tenantID, namespace string, owner metav1.OwnerReference, update bool) error {
	dep, err := p.buildTenantPool(tenantID, namespace)
	if err != nil {
		return err
	}
	applyOwnerRef(&dep.ObjectMeta, owner)
	rendered, err := json.Marshal(dep)
	if err != nil {
		return fmt.Errorf("encode pool deployment: %w", err)
	}
	// The policy is ensured on every apply. A deleted policy would leave pool pods open to any mesh caller.
	if err = p.ensureSessionManagerPolicy(ctx, agentAuthorizationPolicyName(poolName), namespace, map[string]any{poolLabel: "true"}, owner, update); err != nil {
		return fmt.Errorf("ensure pool authorizationpolicy: %w", err)
	}
	key, sum := namespace, sha256.Sum256(rendered)
	if last, ok := p.poolsApplied.Load(key); ok && last == sum {
		return nil
	}
	if err = p.ensureServiceAccount(ctx, poolName, namespace, dep.Labels, owner, update); err != nil {
		return fmt.Errorf("ensure pool serviceaccount: %w", err)
	}
	written, err := p.applyTenantPool(ctx, dep, owner, update)
	if err != nil {
		return err
	}
	// An existing pool left alone may differ from the render, so only a write is remembered.
	if written {
		p.poolsApplied.Store(key, sum)
	}
	return nil
}

// applyTenantPool creates the pool Deployment, or on update replaces an existing
// one. written is false when an existing pool was left alone.
func (p *K8sProvisioner) applyTenantPool(ctx context.Context, dep appsv1.Deployment, owner metav1.OwnerReference, update bool) (written bool, err error) {
	deployments := p.client.AppsV1().Deployments(dep.Namespace)
	_, err = deployments.Create(ctx, &dep, metav1.CreateOptions{})
	if err == nil || !apierrors.IsAlreadyExists(err) {
		return err == nil, err
	}
	if !update {
		return false, nil
	}
	return true, retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, err := deployments.Get(ctx, poolName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get pool deployment: %w", err)
		}
		existing.Labels = dep.Labels
		existing.Spec.Template = dep.Spec.Template
		existing.Spec.Replicas = dep.Spec.Replicas
		applyOwnerRef(&existing.ObjectMeta, owner)
		_, err = deployments.Update(ctx, existing, metav1.UpdateOptions{})
		return err
	})
}
