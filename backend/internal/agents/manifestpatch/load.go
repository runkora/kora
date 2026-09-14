package manifestpatch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

// Load reads every *.yaml/*.yml file under dir (non-recursive), autodetects
// each file's flavor (StrategicMerge or JSONPatch), and returns them in
// lexicographic filename order.
//
// A missing directory returns (nil, nil) — zero patches is a valid state.
// A malformed file is a hard error; the filename is wrapped in the error.
func Load(dir string) ([]Patch, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read patch dir %s: %w", dir, err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		files = append(files, name)
	}
	sort.Strings(files)

	var patches []Patch
	for _, f := range files {
		full := filepath.Join(dir, f)
		body, err := os.ReadFile(full)
		if err != nil {
			return nil, fmt.Errorf("read patch %s: %w", f, err)
		}
		p, err := parse(f, body)
		if err != nil {
			return nil, fmt.Errorf("parse patch %s: %w", f, err)
		}
		patches = append(patches, p)
	}
	return patches, nil
}

// jsonPatchEnvelope is the on-disk YAML shape for a JSON6902 patch. Presence
// of both target and patch keys autodetects the flavor.
type jsonPatchEnvelope struct {
	Target *targetSelector `json:"target,omitempty"`
	Patch  json.RawMessage `json:"patch,omitempty"`
}

type targetSelector struct {
	Kind               string `json:"kind,omitempty"`
	Name               string `json:"name,omitempty"`
	LabelSelector      string `json:"labelSelector,omitempty"`
	AnnotationSelector string `json:"annotationSelector,omitempty"`
}

// strategicMergeHead is the minimum shape needed to identify the target of a
// strategic merge patch.
type strategicMergeHead struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Metadata   *struct {
		Name string `json:"name,omitempty"`
	} `json:"metadata,omitempty"`
}

func parse(file string, body []byte) (Patch, error) {
	bodyJSON, err := yaml.YAMLToJSON(body)
	if err != nil {
		return Patch{}, fmt.Errorf("yaml -> json: %w", err)
	}

	// JSON6902: {target, patch} envelope. Autodetect by non-empty patch array.
	var env jsonPatchEnvelope
	if envErr := json.Unmarshal(bodyJSON, &env); envErr == nil && len(env.Patch) > 0 {
		if env.Target == nil || env.Target.Kind == "" {
			return Patch{}, fmt.Errorf("json6902: target.kind is required")
		}
		if !isJSONArray(env.Patch) {
			return Patch{}, fmt.Errorf("json6902: patch must be an array of RFC 6902 operations")
		}
		return Patch{
			File:               file,
			Flavor:             JSONPatch,
			Kind:               env.Target.Kind,
			Name:               env.Target.Name,
			LabelSelector:      env.Target.LabelSelector,
			AnnotationSelector: env.Target.AnnotationSelector,
			Body:               []byte(env.Patch),
		}, nil
	}

	// Strategic merge: partial kubernetes object.
	var head strategicMergeHead
	if headErr := json.Unmarshal(bodyJSON, &head); headErr != nil {
		return Patch{}, fmt.Errorf("unmarshal strategic merge head: %w", headErr)
	}
	if head.Kind == "" {
		return Patch{}, fmt.Errorf("strategic merge: kind is required")
	}
	var name string
	if head.Metadata != nil {
		name = head.Metadata.Name
	}
	// Strip apiVersion+kind so StrategicMergePatch treats the body as a
	// partial spec against the caller-supplied typed instance.
	stripped, err := stripHead(bodyJSON)
	if err != nil {
		return Patch{}, err
	}
	return Patch{
		File:   file,
		Flavor: StrategicMerge,
		Kind:   head.Kind,
		Name:   name,
		Body:   stripped,
	}, nil
}

func isJSONArray(raw json.RawMessage) bool {
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		}
		return b == '['
	}
	return false
}

// stripHead removes apiVersion and kind from a JSON object so
// strategicpatch.StrategicMergePatch does not fight the typed instance's own
// type discriminators.
func stripHead(bodyJSON []byte) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(bodyJSON, &raw); err != nil {
		return nil, fmt.Errorf("strip head: %w", err)
	}
	delete(raw, "apiVersion")
	delete(raw, "kind")
	out, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("strip head remarshal: %w", err)
	}
	return out, nil
}
