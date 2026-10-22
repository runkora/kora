// Mesh registration of external MCP server hosts: one ServiceEntry per host in
// the system namespace, so the tool-gateway reaches them on a REGISTRY_ONLY
// mesh. A tenant CR cannot own a system-namespace object, so the aggregator
// prunes them by label. See docs/adr/2026-10-22-mcp-server-static-credentials.md.
package operator

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

const (
	// mcpServerHostComponent labels the ServiceEntries this file owns.
	mcpServerHostComponent = "mcp-server-host"
	componentLabel         = "orbit.io/component"
)

// mcpServiceEntries builds one ServiceEntry per external host among the
// registered servers' endpoints. Endpoints it cannot register are skipped.
func mcpServiceEntries(entries []mcpServerRegistryEntry) ([]*unstructured.Unstructured, []mcpServerSkip) {
	byHost := map[string][]providerEndpoint{}
	var skipped []mcpServerSkip
	for _, e := range entries {
		eps, err := urlEndpoints(e.Endpoint)
		if err != nil {
			skipped = append(skipped, mcpServerSkip{e.Name, e.namespace, err.Error()})
			continue
		}
		for _, ep := range eps {
			ep.host = strings.ToLower(ep.host)
			have := byHost[ep.host]
			// Every host in a ServiceEntry shares its ports, so one port number has one protocol.
			if slices.ContainsFunc(have, func(o providerEndpoint) bool { return o.port == ep.port && o.plain != ep.plain }) {
				skipped = append(skipped, mcpServerSkip{e.Name, e.namespace, fmt.Sprintf("port %d of %s is used for both http and https", ep.port, ep.host)})
				continue
			}
			if !slices.Contains(have, ep) {
				byHost[ep.host] = append(have, ep)
			}
		}
	}

	hosts := make([]string, 0, len(byHost))
	for h := range byHost {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	out := make([]*unstructured.Unstructured, 0, len(hosts))
	labels := map[string]any{"app.kubernetes.io/managed-by": "orbit-operator", componentLabel: mcpServerHostComponent}
	for _, h := range hosts {
		name := "mcp-server-" + h
		if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
			skipped = append(skipped, mcpServerSkip{Name: h, Reason: "host cannot name a ServiceEntry: " + strings.Join(errs, "; ")})
			continue
		}
		eps := byHost[h]
		sort.Slice(eps, func(i, j int) bool { return eps[i].port < eps[j].port })
		out = append(out, externalServiceEntry(name, systemNamespace, labels, eps))
	}
	return out, skipped
}

// syncServiceEntries applies the desired ServiceEntries and deletes labeled
// ones no registered server uses.
func (a *MCPServerAggregator) syncServiceEntries(ctx context.Context, entries []mcpServerRegistryEntry) error {
	desired, skipped := mcpServiceEntries(entries)
	for _, s := range skipped {
		a.logger.Warn(ctx, "MCP server host not registered with the mesh",
			logging.Field{Key: "mcpserver", Value: s.Name},
			logging.Field{Key: "namespace", Value: s.Namespace},
			logging.Field{Key: "reason", Value: s.Reason},
		)
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(serviceEntryGVK.GroupVersion().WithKind(serviceEntryGVK.Kind + "List"))
	if err := a.client.List(ctx, list, client.InNamespace(systemNamespace), client.MatchingLabels{componentLabel: mcpServerHostComponent}); err != nil {
		return fmt.Errorf("list MCP server ServiceEntries: %w", err)
	}
	existing := make(map[string]*unstructured.Unstructured, len(list.Items))
	for i := range list.Items {
		existing[list.Items[i].GetName()] = &list.Items[i]
	}

	for _, se := range desired {
		name := se.GetName()
		have, ok := existing[name]
		delete(existing, name)
		if ok && equality.Semantic.DeepEqual(have.Object["spec"], se.Object["spec"]) {
			continue
		}
		if !ok {
			if err := a.client.Create(ctx, se); err == nil {
				continue
			} else if !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("create ServiceEntry %q: %w", name, err)
			}
		}
		if err := a.client.Patch(ctx, se, client.Merge, client.FieldOwner("orbit-operator")); err != nil {
			return fmt.Errorf("patch ServiceEntry %q: %w", name, err)
		}
	}
	for name, stale := range existing {
		if err := a.client.Delete(ctx, stale); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete ServiceEntry %q: %w", name, err)
		}
	}
	return nil
}
