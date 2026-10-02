package manifestpatch

import (
	"fmt"
	"reflect"
	"strings"

	"sigs.k8s.io/yaml"
)

// protectedPaths are fields a patch must leave exactly as orbit-agents built
// them: resource identity and wiring, pod and container security context,
// what the pod exposes to an agent, and the sandbox's per-run workspace mount. "*" matches every list element.
var protectedPaths = [][]string{
	{"apiVersion"},
	{"kind"},
	{"metadata", "name"},
	{"metadata", "namespace"},
	{"metadata", "ownerReferences"},
	{"spec", "selector"},
	{"spec", "serviceName"},
	{"spec", "scaleTargetRef"},
	{"spec", "volumeClaimTemplates", "*", "metadata", "name"},
	{"spec", "serviceAccountName"},
	{"spec", "template", "spec", "serviceAccountName"},
	{"spec", "automountServiceAccountToken"},
	{"spec", "template", "spec", "automountServiceAccountToken"},
	{"spec", "enableServiceLinks"},
	{"spec", "template", "spec", "enableServiceLinks"},
	{"spec", "securityContext", "runAsNonRoot"},
	{"spec", "securityContext", "runAsUser"},
	{"spec", "securityContext", "seccompProfile"},
	{"spec", "template", "spec", "securityContext", "runAsNonRoot"},
	{"spec", "template", "spec", "securityContext", "runAsUser"},
	{"spec", "template", "spec", "securityContext", "seccompProfile"},
	{"spec", "containers", "*", "securityContext"},
	{"spec", "template", "spec", "containers", "*", "securityContext"},
	{"spec", "volumes", "*", "persistentVolumeClaim", "claimName"},
	{"spec", "containers", "*", "volumeMounts"},
	{"spec", "initContainers", "*", "volumeMounts"},
}

// IdentityLabels select a pod for SPIRE attestation and sidecar injection.
// Changing one moves the pod off its attested identity. They are protected on
// the resource and on the StatefulSet pod template, and are equally off limits
// to the first-class pod labels in agents.WorkloadsConfig.
var IdentityLabels = []string{
	"spiffe.io/agent",
	"spiffe.io/spire-managed-identity",
	"orbit.io/identity-proxy",
	"orbit.io/tenant",
	"orbit.io/agent-type",
	"orbit.io/role",
	"orbit.io/run-id",
	"orbit.io/agent-slug",
}

// checkProtected rejects a patched resource whose protected fields differ
// from the built one.
func checkProtected(built, patched []byte) error {
	var before, after map[string]any
	if err := yaml.Unmarshal(built, &before); err != nil {
		return fmt.Errorf("decode built resource: %w", err)
	}
	if err := yaml.Unmarshal(patched, &after); err != nil {
		return fmt.Errorf("decode patched resource: %w", err)
	}
	for _, path := range protectedPaths {
		if err := unchanged(before, after, path); err != nil {
			return err
		}
	}
	for _, label := range IdentityLabels {
		if err := unchanged(before, after, []string{"metadata", "labels", label}); err != nil {
			return err
		}
		if err := unchanged(before, after, []string{"spec", "template", "metadata", "labels", label}); err != nil {
			return err
		}
	}
	return nil
}

// unchanged reports an error when the values at path differ.
func unchanged(before, after map[string]any, path []string) error {
	if !reflect.DeepEqual(collect(before, path), collect(after, path)) {
		return fmt.Errorf("changes protected field %s", strings.Join(path, "."))
	}
	return nil
}

// collect returns every value at path in v, skipping branches where the path
// does not exist.
func collect(v any, path []string) []any {
	if len(path) == 0 {
		return []any{v}
	}
	switch node := v.(type) {
	case map[string]any:
		child, ok := node[path[0]]
		if !ok {
			return nil
		}
		return collect(child, path[1:])
	case []any:
		if path[0] != "*" {
			return nil
		}
		var out []any
		for _, el := range node {
			out = append(out, collect(el, path[1:])...)
		}
		return out
	}
	return nil
}
