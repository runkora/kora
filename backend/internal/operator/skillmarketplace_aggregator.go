// SkillMarketplace aggregator.
//
// skills-service runs in kora-system and reads every tenant's Marketplaces from
// mounted files. Pods mount only ConfigMaps and Secrets of their own namespace,
// so the operator renders every SkillMarketplace into two kora-system objects:
//
//   - ConfigMap kora-skill-marketplaces: each tenant's Marketplaces with
//     repository, ref, App and the data key of the private key, in the shape
//     internal/platform/marketplace defines.
//   - Secret kora-skill-marketplace-secrets: each App private key, copied from
//     the Marketplace's namespace.
//
// A verdict depends on sibling Marketplaces (repository clashes), so every run
// computes a verdict for every Marketplace and patches each status that
// changed. The aggregator makes no GitHub calls.
//
// See docs/adr/2026-08-17-agent-skills.md.
package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/golang-jwt/jwt/v5"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/marketplace"
)

// ErrMarketplaceCredentialUnavailable is returned when a Marketplace's private
// key cannot be used. The Marketplace stays listed and browsing fails closed.
var ErrMarketplaceCredentialUnavailable = errors.New("credential unavailable")

// maxMarketplaceKeySize bounds one private key. Every key shares one Secret,
// which Kubernetes caps at 1 MiB.
const maxMarketplaceKeySize = 16 << 10

// marketplaceVerdict is one Marketplace's outcome of a run, written to its
// status as the phase and the Aggregated condition.
type marketplaceVerdict struct {
	phase   string
	reason  string
	message string
}

func (v marketplaceVerdict) aggregated() bool {
	return v.phase == SkillMarketplacePhaseAggregated
}

// buildMarketplaces is the pure core of aggregation. Checks run in order:
//
//  1. In kora-system: Invalid, GlobalSourcesNotSupported.
//  2. No served tenant owns the namespace: Pending, TenantNotActive.
//  3. An older Marketplace in the namespace holds the repository: Invalid,
//     DuplicateRepository.
//  4. Otherwise: Aggregated. Sync downgrades the reason to
//     CredentialUnavailable when the key cannot be read.
//
// Older is declaredBefore. Each tenant's Marketplaces render oldest first,
// which is the order skills-service lists them in.
func buildMarketplaces(items []SkillMarketplace, tenants []KoraTenant) (marketplace.File, map[client.ObjectKey]marketplaceVerdict) {
	owner := map[string]string{}
	for _, t := range servedTenants(tenants) {
		owner[tenantNamespace(&t)] = t.Name
	}

	ordered := make([]*SkillMarketplace, len(items))
	for i := range items {
		ordered[i] = &items[i]
	}
	sort.SliceStable(ordered, func(i, j int) bool { return declaredBefore(ordered[i], ordered[j]) })

	rendered := map[string][]marketplace.Marketplace{}
	verdicts := make(map[client.ObjectKey]marketplaceVerdict, len(items))
	heldRepos := map[string]map[string]string{} // namespace -> owner/repo -> holder

	for _, m := range ordered {
		key := client.ObjectKeyFromObject(m)
		if m.Namespace == systemNamespace {
			verdicts[key] = marketplaceVerdict{SkillMarketplacePhaseInvalid, MarketplaceReasonGlobalSourcesNotSupported,
				fmt.Sprintf("Marketplaces in %s are not supported; apply it in a tenant namespace", systemNamespace)}
			continue
		}
		tenant, ok := owner[m.Namespace]
		if !ok {
			verdicts[key] = marketplaceVerdict{SkillMarketplacePhasePending, MarketplaceReasonTenantNotActive,
				fmt.Sprintf("namespace %q has no active KoraTenant", m.Namespace)}
			continue
		}
		if heldRepos[m.Namespace] == nil {
			heldRepos[m.Namespace] = map[string]string{}
		}
		repo := m.Spec.GitHub.repoKey()
		if holder, taken := heldRepos[m.Namespace][repo]; taken {
			verdicts[key] = marketplaceVerdict{SkillMarketplacePhaseInvalid, MarketplaceReasonDuplicateRepository,
				fmt.Sprintf("%s is already registered by older Marketplace %q", repo, holder)}
			continue
		}
		heldRepos[m.Namespace][repo] = m.Name

		gh := m.Spec.GitHub
		rendered[tenant] = append(rendered[tenant], marketplace.Marketplace{
			Slug:                m.Name,
			Owner:               gh.Owner,
			Repo:                gh.Repo,
			Ref:                 gh.Ref,
			AppID:               gh.App.AppID,
			InstallationID:      gh.App.InstallationID,
			PrivateKeySecretKey: marketplace.SecretKey(m.Namespace, m.Name),
		})
		verdicts[key] = marketplaceVerdict{SkillMarketplacePhaseAggregated, MarketplaceReasonAggregated,
			fmt.Sprintf("rendered for tenant %q into %s", tenant, marketplace.ConfigMapName)}
	}

	ids := make([]string, 0, len(rendered))
	for id := range rendered {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	file := marketplace.File{Tenants: make([]marketplace.Tenant, 0, len(ids))}
	for _, id := range ids {
		file.Tenants = append(file.Tenants, marketplace.Tenant{ID: id, Marketplaces: rendered[id]})
	}
	return file, verdicts
}

// SkillMarketplaceAggregator renders every served SkillMarketplace into the
// kora-system ConfigMap and Secret and keeps each status in step with its
// verdict. The SkillMarketplace and tenant reconcilers both run it.
type SkillMarketplaceAggregator struct {
	client client.Client
	logger logging.Logger
}

// NewSkillMarketplaceAggregator creates a SkillMarketplaceAggregator. Secrets
// are read through c, which the operator configures to bypass its cache.
func NewSkillMarketplaceAggregator(c client.Client, logger logging.Logger) *SkillMarketplaceAggregator {
	return &SkillMarketplaceAggregator{client: c, logger: logger}
}

// Sync writes the Secret, then the ConfigMap, then each changed status, so a
// status never reads Aggregated before skills-service can see the Marketplace.
func (a *SkillMarketplaceAggregator) Sync(ctx context.Context) error {
	tenants := &OrbitTenantList{}
	if err := a.client.List(ctx, tenants); err != nil {
		return fmt.Errorf("list OrbitTenants: %w", err)
	}
	items := &SkillMarketplaceList{}
	if err := a.client.List(ctx, items); err != nil {
		return fmt.Errorf("list SkillMarketplaces: %w", err)
	}

	file, verdicts := buildMarketplaces(items.Items, tenants.Items)
	keys, readErr := a.collectKeys(ctx, items.Items, verdicts)
	secretErr := a.upsertSecret(ctx, keys)
	if err := a.upsertConfigMap(ctx, file); err != nil {
		return err
	}
	return errors.Join(readErr, secretErr, a.updateStatuses(ctx, items.Items, verdicts))
}

// collectKeys reads the private key of every Marketplace being served. An
// unusable key turns the verdict into CredentialUnavailable. A failed read
// keeps the key already aggregated and is returned, so the reconcile retries.
func (a *SkillMarketplaceAggregator) collectKeys(ctx context.Context, items []SkillMarketplace, verdicts map[client.ObjectKey]marketplaceVerdict) (map[string][]byte, error) {
	previous := a.aggregatedKeys(ctx)
	data := map[string][]byte{}
	var errs []error
	for i := range items {
		m := &items[i]
		objKey := client.ObjectKeyFromObject(m)
		v := verdicts[objKey]
		if !v.aggregated() {
			continue
		}
		dataKey := marketplace.SecretKey(m.Namespace, m.Name)
		pem, err := readMarketplaceKey(ctx, a.client, m.Namespace, m.Spec.GitHub.App.PrivateKeySecretRef)
		switch {
		case err == nil:
			data[dataKey] = pem
		case errors.Is(err, ErrMarketplaceCredentialUnavailable):
			verdicts[objKey] = marketplaceVerdict{v.phase, MarketplaceReasonCredentialUnavailable, v.message + "; " + err.Error()}
		default:
			if old, ok := previous[dataKey]; ok {
				data[dataKey] = old
			}
			errs = append(errs, fmt.Errorf("SkillMarketplace %s: %w", objKey, err))
		}
	}
	return data, errors.Join(errs...)
}

// aggregatedKeys returns the keys in the aggregated Secret, or nil when it
// cannot be read.
func (a *SkillMarketplaceAggregator) aggregatedKeys(ctx context.Context) map[string][]byte {
	s := &corev1.Secret{}
	if err := a.client.Get(ctx, client.ObjectKey{Namespace: systemNamespace, Name: marketplace.SecretName}, s); err != nil {
		return nil
	}
	return s.Data
}

// readMarketplaceKey returns the PEM private key ref names in namespace. An
// unusable key wraps ErrMarketplaceCredentialUnavailable. No error carries the
// key.
func readMarketplaceKey(ctx context.Context, c client.Reader, namespace string, ref SkillMarketplaceSecretRef) ([]byte, error) {
	secret := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: Secret %q not found", ErrMarketplaceCredentialUnavailable, ref.Name)
		}
		return nil, fmt.Errorf("read Secret %q: %w", ref.Name, err)
	}
	// Operator-managed Secrets hold platform credentials a tenant must not use.
	if secret.Labels["app.kubernetes.io/managed-by"] == "kora-operator" {
		return nil, fmt.Errorf("%w: Secret %q is managed by the operator", ErrMarketplaceCredentialUnavailable, ref.Name)
	}
	v, ok := secret.Data[ref.Key]
	switch {
	case !ok:
		return nil, fmt.Errorf("%w: Secret %q has no key %q", ErrMarketplaceCredentialUnavailable, ref.Name, ref.Key)
	case len(v) > maxMarketplaceKeySize:
		return nil, fmt.Errorf("%w: Secret %q key %q is over %d bytes", ErrMarketplaceCredentialUnavailable, ref.Name, ref.Key, maxMarketplaceKeySize)
	}
	if _, err := jwt.ParseRSAPrivateKeyFromPEM(v); err != nil {
		return nil, fmt.Errorf("%w: Secret %q key %q is not a PEM RSA private key", ErrMarketplaceCredentialUnavailable, ref.Name, ref.Key)
	}
	return v, nil
}

// updateStatuses logs every Marketplace not served cleanly and patches each
// status whose verdict changed. One failed patch does not stop the others.
func (a *SkillMarketplaceAggregator) updateStatuses(ctx context.Context, items []SkillMarketplace, verdicts map[client.ObjectKey]marketplaceVerdict) error {
	var errs []error
	for i := range items {
		m := &items[i]
		v := verdicts[client.ObjectKeyFromObject(m)]
		if v.reason != MarketplaceReasonAggregated {
			a.logger.Warn(ctx, "SkillMarketplace not served",
				logging.Field{Key: "marketplace", Value: m.Name},
				logging.Field{Key: "namespace", Value: m.Namespace},
				logging.Field{Key: "reason", Value: v.reason},
				logging.Field{Key: "detail", Value: v.message},
			)
		}
		if marketplaceStatusMatches(m, v) {
			continue
		}
		if err := a.setStatus(ctx, m, v); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func conditionStatus(v marketplaceVerdict) metav1.ConditionStatus {
	if v.aggregated() {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}

// marketplaceStatusMatches reports whether m's status already records v for
// its current generation, so an unchanged verdict costs no API write.
func marketplaceStatusMatches(m *SkillMarketplace, v marketplaceVerdict) bool {
	cond := meta.FindStatusCondition(m.Status.Conditions, conditionAggregated)
	return m.Status.Phase == v.phase &&
		cond != nil &&
		cond.Status == conditionStatus(v) &&
		cond.Reason == v.reason &&
		cond.Message == v.message &&
		cond.ObservedGeneration == m.Generation
}

// setStatus patches the phase, LastReconciledAt and the Aggregated condition.
func (a *SkillMarketplaceAggregator) setStatus(ctx context.Context, m *SkillMarketplace, v marketplaceVerdict) error {
	patch := client.MergeFrom(m.DeepCopy())
	now := metav1.Now()
	m.Status.Phase = v.phase
	m.Status.LastReconciledAt = &now
	meta.SetStatusCondition(&m.Status.Conditions, metav1.Condition{
		Type:               conditionAggregated,
		Status:             conditionStatus(v),
		ObservedGeneration: m.Generation,
		Reason:             v.reason,
		Message:            v.message,
	})
	// A Marketplace deleted since the list has nothing left to report on.
	if err := a.client.Status().Patch(ctx, m, patch); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("patch SkillMarketplace %s/%s status: %w", m.Namespace, m.Name, err)
	}
	return nil
}

func marketplaceLabels(component string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "kora-operator",
		"runkora.io/component":         component,
	}
}

// upsertSecret creates or replaces the aggregated keys Secret.
func (a *SkillMarketplaceAggregator) upsertSecret(ctx context.Context, data map[string][]byte) error {
	existing := &corev1.Secret{}
	err := a.client.Get(ctx, client.ObjectKey{Namespace: systemNamespace, Name: marketplace.SecretName}, existing)
	switch {
	case apierrors.IsNotFound(err):
		s := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: marketplace.SecretName, Namespace: systemNamespace, Labels: marketplaceLabels("skill-marketplace-secrets")},
			Type:       corev1.SecretTypeOpaque,
			Data:       data,
		}
		if err := a.client.Create(ctx, s); err != nil {
			return fmt.Errorf("create %s Secret: %w", marketplace.SecretName, err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("get %s Secret: %w", marketplace.SecretName, err)
	case dataEqual(existing.Data, data):
		return nil
	}
	// Two controllers share this aggregator. The lock turns a stale writer into
	// a conflict and requeue.
	patch := client.MergeFromWithOptions(existing.DeepCopy(), client.MergeFromWithOptimisticLock{})
	existing.Data = data
	if err := a.client.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("patch %s Secret: %w", marketplace.SecretName, err)
	}
	return nil
}

// upsertConfigMap creates or patches the Marketplaces ConfigMap, skipping an
// unchanged write so the kubelet does not re-project an identical volume.
func (a *SkillMarketplaceAggregator) upsertConfigMap(ctx context.Context, file marketplace.File) error {
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal Marketplaces: %w", err)
	}
	data := string(raw)

	existing := &corev1.ConfigMap{}
	err = a.client.Get(ctx, client.ObjectKey{Namespace: systemNamespace, Name: marketplace.ConfigMapName}, existing)
	switch {
	case apierrors.IsNotFound(err):
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: marketplace.ConfigMapName, Namespace: systemNamespace, Labels: marketplaceLabels("skill-marketplaces")},
			Data:       map[string]string{marketplace.FileName: data},
		}
		if err := a.client.Create(ctx, cm); err != nil {
			return fmt.Errorf("create %s ConfigMap: %w", marketplace.ConfigMapName, err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("get %s ConfigMap: %w", marketplace.ConfigMapName, err)
	case existing.Data[marketplace.FileName] == data:
		return nil
	}
	patch := client.MergeFromWithOptions(existing.DeepCopy(), client.MergeFromWithOptimisticLock{})
	if existing.Data == nil {
		existing.Data = map[string]string{}
	}
	existing.Data[marketplace.FileName] = data
	if err := a.client.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("patch %s ConfigMap: %w", marketplace.ConfigMapName, err)
	}
	return nil
}
