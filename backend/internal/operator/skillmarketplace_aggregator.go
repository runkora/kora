// Skill marketplace source aggregator.
//
// skills-service runs in orbit-system and reads every tenant's marketplaces from
// one mounted file. Pods can only mount ConfigMaps from their own namespace, so
// the operator renders the SkillMarketplaceSource CRs from every tenant
// namespace into one orbit-system ConfigMap:
//
//   - ConfigMap orbit-skill-marketplace-sources, one data key,
//     skills-mapping.yaml, in the shape skills-service already reads and
//     internal/platform/marketplace defines: each tenant's repository links.
//
// A source is rendered only when its owner/repo is on its tenant's
// spec.allowedMarketplaceRepos. Its verdict therefore depends on that field and
// on its sibling sources (repository clashes), not only on its own
// spec, so every run computes a verdict for every source and patches the status
// of each one whose verdict changed. The aggregator makes no GitHub calls.
//
// See docs/adr/2026-10-13-skill-marketplace-source-registration.md.
package operator

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/marketplace"
)

// marketplaceVerdict is one source's outcome of an aggregation run, written to
// its status as the phase and the Aggregated condition.
type marketplaceVerdict struct {
	phase   string
	reason  string
	message string
}

func (v marketplaceVerdict) aggregated() bool {
	return v.phase == SkillMarketplaceSourcePhaseAggregated
}

// buildMarketplaceSources is the pure core of aggregation. Given every
// SkillMarketplaceSource and every OrbitTenant, it returns the file to render
// and a verdict for every source. Checks run in this order:
//
//  1. In orbit-system: Invalid, GlobalSourcesNotSupported.
//  2. No active tenant owns the namespace: Pending, TenantNotActive.
//  3. An older source in the namespace holds the repository: Invalid,
//     DuplicateRepository.
//  4. The repository is not on the tenant's allowlist: Pending, NotAllowlisted.
//  5. Otherwise rendered: Aggregated.
//
// "Older" is (creationTimestamp, metadata.name). A source that passes 3 holds
// its repository even while it waits at 4. Rendered links keep that oldest-first
// order, which is the display order skills-service shows.
func buildMarketplaceSources(sources []SkillMarketplaceSource, tenants []OrbitTenant) (marketplace.SourcesFile, map[client.ObjectKey]marketplaceVerdict) {
	// Two tenants can resolve to one namespace through spec.namespace. The
	// owner must not depend on list order, and an inactive tenant must not hide
	// an active one.
	byNamespace := make(map[string]*OrbitTenant, len(tenants))
	for i := range tenants {
		namespace := tenantNamespace(&tenants[i])
		if held, ok := byNamespace[namespace]; !ok || outranks(&tenants[i], held) {
			byNamespace[namespace] = &tenants[i]
		}
	}

	ordered := make([]*SkillMarketplaceSource, len(sources))
	for i := range sources {
		ordered[i] = &sources[i]
	}
	// Oldest first, so the oldest source keeps a contested name or repository.
	// Creation timestamps have one-second precision: the name breaks ties, or
	// sources applied together would win at random on each run.
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if !a.CreationTimestamp.Equal(&b.CreationTimestamp) {
			return a.CreationTimestamp.Before(&b.CreationTimestamp)
		}
		return a.Name < b.Name
	})

	links := map[string][]string{} // tenant → rendered repository links, oldest first
	verdicts := make(map[client.ObjectKey]marketplaceVerdict, len(sources))
	// A repository clash is checked within one namespace; two tenants may
	// register the same repository.
	heldRepos := map[string]map[string]string{} // namespace → owner/repo → holder

	for _, src := range ordered {
		key := client.ObjectKeyFromObject(src)

		if src.Namespace == systemNamespace {
			verdicts[key] = marketplaceVerdict{SkillMarketplaceSourcePhaseInvalid, MarketplaceReasonGlobalSourcesNotSupported,
				fmt.Sprintf("global marketplace sources (applied in %s) are not supported; apply it in a tenant namespace", systemNamespace)}
			continue
		}
		tenant, ok := byNamespace[src.Namespace]
		if !ok || !tenant.Spec.Active {
			verdicts[key] = marketplaceVerdict{SkillMarketplaceSourcePhasePending, MarketplaceReasonTenantNotActive,
				fmt.Sprintf("namespace %q has no active OrbitTenant", src.Namespace)}
			continue
		}

		if heldRepos[src.Namespace] == nil {
			heldRepos[src.Namespace] = map[string]string{}
		}
		repo := src.Spec.GitHub.repoKey()
		if holder, taken := heldRepos[src.Namespace][repo]; taken {
			verdicts[key] = marketplaceVerdict{SkillMarketplaceSourcePhaseInvalid, MarketplaceReasonDuplicateRepository,
				fmt.Sprintf("%s is already registered by older source %q", repo, holder)}
			continue
		}
		heldRepos[src.Namespace][repo] = src.Name

		if !tenantAllowsRepo(tenant, repo) {
			verdicts[key] = marketplaceVerdict{SkillMarketplaceSourcePhasePending, MarketplaceReasonNotAllowlisted,
				fmt.Sprintf("%s is not in OrbitTenant %q spec.allowedMarketplaceRepos; a platform admin must approve it", repo, tenant.Name)}
			continue
		}

		links[tenant.Name] = append(links[tenant.Name], marketplace.RepoURL(src.Spec.GitHub.Owner, src.Spec.GitHub.Repo))
		verdicts[key] = marketplaceVerdict{SkillMarketplaceSourcePhaseAggregated, MarketplaceReasonAggregated,
			fmt.Sprintf("rendered for tenant %q into %s", tenant.Name, marketplace.SourcesConfigMapName)}
	}
	return renderMarketplaceFile(links), verdicts
}

// renderMarketplaceFile lists the tenants in ID order, so the same sources
// always render the same text and an unchanged list costs no write.
func renderMarketplaceFile(links map[string][]string) marketplace.SourcesFile {
	ids := make([]string, 0, len(links))
	for id := range links {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	tenants := make([]marketplace.TenantMarketplaces, 0, len(ids))
	for _, id := range ids {
		tenants = append(tenants, marketplace.TenantMarketplaces{ID: id, Marketplaces: links[id]})
	}
	return marketplace.SourcesFile{SkillsMapping: marketplace.SkillsMapping{Tenants: tenants}}
}

// outranks reports whether tenant a should own a namespace that tenant b also
// claims: an active tenant beats an inactive one, then the lower name wins.
func outranks(a, b *OrbitTenant) bool {
	if a.Spec.Active != b.Spec.Active {
		return a.Spec.Active
	}
	return a.Name < b.Name
}

// tenantAllowsRepo reports whether repo, a lowercase owner/repo, is on the
// tenant's allowlist. GitHub treats owner and repository names
// case-insensitively.
func tenantAllowsRepo(t *OrbitTenant, repo string) bool {
	for _, allowed := range t.Spec.AllowedMarketplaceRepos {
		if strings.EqualFold(allowed, repo) {
			return true
		}
	}
	return false
}

// SkillMarketplaceAggregator renders every allow-listed SkillMarketplaceSource
// into the orbit-skill-marketplace-sources ConfigMap in orbit-system and keeps
// each source's status in step with its verdict. The SkillMarketplaceSource
// reconciler and the tenant reconciler both run it.
type SkillMarketplaceAggregator struct {
	client client.Client
	logger logging.Logger
}

// NewSkillMarketplaceAggregator creates a SkillMarketplaceAggregator backed by
// the provided client.
func NewSkillMarketplaceAggregator(c client.Client, logger logging.Logger) *SkillMarketplaceAggregator {
	return &SkillMarketplaceAggregator{client: c, logger: logger}
}

// Sync computes every source's verdict, writes the ConfigMap, then patches the
// status of each source whose verdict changed. The ConfigMap is written first,
// so a status never reads Aggregated before skills-service can see the source.
func (a *SkillMarketplaceAggregator) Sync(ctx context.Context) error {
	tenants := &OrbitTenantList{}
	if err := a.client.List(ctx, tenants); err != nil {
		return fmt.Errorf("list OrbitTenants: %w", err)
	}
	sources := &SkillMarketplaceSourceList{}
	if err := a.client.List(ctx, sources); err != nil {
		return fmt.Errorf("list SkillMarketplaceSources: %w", err)
	}

	file, verdicts := buildMarketplaceSources(sources.Items, tenants.Items)
	if err := a.upsertConfigMap(ctx, file); err != nil {
		return err
	}
	return a.updateStatuses(ctx, sources.Items, verdicts)
}

// updateStatuses logs every source left out of the file and patches each
// status whose verdict changed. One failed patch does not stop the others.
func (a *SkillMarketplaceAggregator) updateStatuses(
	ctx context.Context,
	sources []SkillMarketplaceSource,
	verdicts map[client.ObjectKey]marketplaceVerdict,
) error {
	var errs []error
	for i := range sources {
		src := &sources[i]
		v := verdicts[client.ObjectKeyFromObject(src)]
		if !v.aggregated() {
			a.logger.Warn(ctx, "SkillMarketplaceSource excluded from marketplace sources",
				logging.Field{Key: "source", Value: src.Name},
				logging.Field{Key: "namespace", Value: src.Namespace},
				logging.Field{Key: "reason", Value: v.reason},
				logging.Field{Key: "detail", Value: v.message},
			)
		}
		if statusMatches(src, v) {
			continue
		}
		if err := a.setStatus(ctx, src, v); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// statusMatches reports whether src's status already records v for its
// current generation, so an unchanged verdict costs no API write.
func statusMatches(src *SkillMarketplaceSource, v marketplaceVerdict) bool {
	cond := meta.FindStatusCondition(src.Status.Conditions, conditionAggregated)
	wantStatus := metav1.ConditionFalse
	if v.aggregated() {
		wantStatus = metav1.ConditionTrue
	}
	return src.Status.Phase == v.phase &&
		cond != nil &&
		cond.Status == wantStatus &&
		cond.Reason == v.reason &&
		cond.Message == v.message &&
		cond.ObservedGeneration == src.Generation
}

// setStatus patches the phase, LastReconciledAt and the Aggregated condition.
func (a *SkillMarketplaceAggregator) setStatus(ctx context.Context, src *SkillMarketplaceSource, v marketplaceVerdict) error {
	patch := client.MergeFrom(src.DeepCopy())
	now := metav1.Now()
	status := metav1.ConditionFalse
	if v.aggregated() {
		status = metav1.ConditionTrue
	}
	src.Status.Phase = v.phase
	src.Status.LastReconciledAt = &now
	meta.SetStatusCondition(&src.Status.Conditions, metav1.Condition{
		Type:               conditionAggregated,
		Status:             status,
		ObservedGeneration: src.Generation,
		Reason:             v.reason,
		Message:            v.message,
	})
	// A source deleted since the list has nothing left to report on.
	if err := a.client.Status().Patch(ctx, src, patch); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("patch SkillMarketplaceSource %s/%s status: %w", src.Namespace, src.Name, err)
	}
	return nil
}

// upsertConfigMap creates or patches the marketplace sources ConfigMap,
// skipping the write when the content is unchanged so the kubelet does not
// re-project an identical volume.
func (a *SkillMarketplaceAggregator) upsertConfigMap(ctx context.Context, file marketplace.SourcesFile) error {
	raw, err := yaml.Marshal(file)
	if err != nil {
		return fmt.Errorf("marshal marketplace sources: %w", err)
	}
	data := string(raw)

	existing := &corev1.ConfigMap{}
	getErr := a.client.Get(ctx, client.ObjectKey{
		Namespace: systemNamespace,
		Name:      marketplace.SourcesConfigMapName,
	}, existing)

	switch {
	case apierrors.IsNotFound(getErr):
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      marketplace.SourcesConfigMapName,
				Namespace: systemNamespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "orbit-operator",
					"orbit.io/component":           "skill-marketplace-sources",
				},
			},
			Data: map[string]string{marketplace.SourcesFileName: data},
		}
		if err := a.client.Create(ctx, cm); err != nil {
			return fmt.Errorf("create %s ConfigMap: %w", marketplace.SourcesConfigMapName, err)
		}
		a.logger.Info(ctx, "created skill marketplace sources ConfigMap",
			logging.Field{Key: "configmap", Value: marketplace.SourcesConfigMapName},
			logging.Field{Key: "namespace", Value: systemNamespace},
			logging.Field{Key: "tenants", Value: len(file.SkillsMapping.Tenants)},
		)
		return nil
	case getErr != nil:
		return fmt.Errorf("get %s ConfigMap: %w", marketplace.SourcesConfigMapName, getErr)
	default:
		if existing.Data[marketplace.SourcesFileName] == data {
			a.logger.Debug(ctx, "aggregated skill marketplace sources unchanged, skipping update")
			return nil
		}
		// Two controllers share this aggregator; the lock turns a stale writer
		// into a conflict and requeue instead of a silent overwrite.
		patch := client.MergeFromWithOptions(existing.DeepCopy(), client.MergeFromWithOptimisticLock{})
		if existing.Data == nil {
			existing.Data = make(map[string]string)
		}
		existing.Data[marketplace.SourcesFileName] = data
		if err := a.client.Patch(ctx, existing, patch); err != nil {
			return fmt.Errorf("patch %s ConfigMap: %w", marketplace.SourcesConfigMapName, err)
		}
		a.logger.Info(ctx, "updated skill marketplace sources ConfigMap",
			logging.Field{Key: "configmap", Value: marketplace.SourcesConfigMapName},
			logging.Field{Key: "namespace", Value: systemNamespace},
			logging.Field{Key: "tenants", Value: len(file.SkillsMapping.Tenants)},
		)
		return nil
	}
}
