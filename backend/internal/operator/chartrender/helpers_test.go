package chartrender

// Base fixture values satisfying every `required` value in each chart, so
// helm template renders successfully; test-specific overrides are merged on
// top via mergeValues.

import "maps"

func orbitPlatformBaseValues() map[string]any {
	return map[string]any{
		"frontendHost": "orbit.test",
	}
}

func orbitBaseValues() map[string]any {
	return map[string]any{
		"frontendHost": "orbit.test",
		"frontend": map[string]any{
			"config": map[string]any{
				"orbitApiUrl":         "https://api.orbit.test",
				"orbitTriggersUrl":    "https://triggers.orbit.test",
				"inferenceGatewayUrl": "https://inference.orbit.test",
				"policyGatewayUrl":    "https://policy.orbit.test",
				"policyServiceUrl":    "https://policy-service.orbit.test",
				"orbitAgentsUrl":      "https://agents.orbit.test",
				"toolGatewayUrl":      "https://tools.orbit.test",
				"nextPublicAppUrl":    "https://orbit.test",
			},
		},
	}
}

// mergeValues recursively overlays src onto dst (src wins), same semantics
// as the operator package's own mergeMaps — duplicated here rather than
// imported to keep this test-only harness independent of operator's
// production wiring.
func mergeValues(dst, src map[string]any) map[string]any {
	out := make(map[string]any, len(dst))
	maps.Copy(out, dst)
	for k, v := range src {
		if vMap, ok := v.(map[string]any); ok {
			if dstMap, ok := out[k].(map[string]any); ok {
				out[k] = mergeValues(dstMap, vMap)
				continue
			}
		}
		out[k] = v
	}
	return out
}
