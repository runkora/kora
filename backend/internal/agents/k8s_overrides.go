package agents

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/sjson"
)

type resourceOverride struct {
	name  string // agent type slug or "*" for all
	path  string // dot-separated JSON path, e.g. "spec.storageClassName"
	value string // raw value from env var
}

// ManifestOverrides groups field overrides by Kubernetes Kind name.
// Populated from ORBIT_MANIFEST__<Kind>__<name>__<path>=<value> env vars.
type ManifestOverrides map[string][]resourceOverride

// ParseManifestOverrides scans environ (typically os.Environ()) for entries
// matching ORBIT_MANIFEST__<Kind>__<name>__<path>=<value>.
//
// Format:
//   - <Kind>  — full Kubernetes Kind name (e.g. StatefulSet, PersistentVolumeClaim)
//   - <name>  — agent type slug to target, or "*" to match all agent types
//   - <path>  — remaining "__"-separated segments become the dot-notation JSON path
//
// Example env vars:
//
//	ORBIT_MANIFEST__PersistentVolumeClaim__*__spec__storageClassName=premium-ssd
//	ORBIT_MANIFEST__StatefulSet__coder__spec__replicas=5
//
// Entries with fewer than 3 segments (Kind + name + at least one path segment)
// are skipped; their key names are returned in the second value so callers can
// emit a warning without failing startup.
func ParseManifestOverrides(environ []string) (ManifestOverrides, []string) {
	overrides := make(ManifestOverrides)
	var malformed []string
	for _, e := range environ {
		key, val, _ := strings.Cut(e, "=")
		if !strings.HasPrefix(key, "ORBIT_MANIFEST__") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(key, "ORBIT_MANIFEST__"), "__")
		if len(parts) < 3 { // Kind + name + at least one path segment required
			malformed = append(malformed, key)
			continue
		}
		kind, name := parts[0], parts[1]
		path := strings.Join(parts[2:], ".")
		overrides[kind] = append(overrides[kind], resourceOverride{name: name, path: path, value: val})
	}
	return overrides, malformed
}

// overridesForSlug returns the overrides for a given agent slug from a Kind's list.
// Overrides with name "*" match any slug; others only match their exact slug.
func overridesForSlug(overrides []resourceOverride, slug string) []resourceOverride {
	var matched []resourceOverride
	for _, ov := range overrides {
		if ov.name == "*" || ov.name == slug {
			matched = append(matched, ov)
		}
	}
	return matched
}

// applyOverrides applies field overrides to a typed Kubernetes object via a JSON
// round-trip. Values that are valid JSON (numbers, booleans, null, objects, arrays)
// are inserted as their native JSON type; all other values are treated as strings.
func applyOverrides[T any](obj T, overrides []resourceOverride) (T, error) {
	if len(overrides) == 0 {
		return obj, nil
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return obj, fmt.Errorf("marshal for override: %w", err)
	}
	for _, ov := range overrides {
		if json.Valid([]byte(ov.value)) {
			data, err = sjson.SetRawBytes(data, ov.path, []byte(ov.value))
		} else {
			data, err = sjson.SetBytes(data, ov.path, ov.value)
		}
		if err != nil {
			return obj, fmt.Errorf("apply override %q: %w", ov.path, err)
		}
	}
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return obj, fmt.Errorf("unmarshal after overrides: %w", err)
	}
	return result, nil
}
