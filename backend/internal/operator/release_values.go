package operator

func buildSPIRECRDsValues(overrides map[string]any) map[string]any {
	return mergeMaps(map[string]any{}, overrides)
}

func buildSPIREValues(p *OrbitPlatform) map[string]any {
	return mergeMaps(map[string]any{}, p.Spec.SpireValues)
}

func buildIstioBaseValues(overrides map[string]any) map[string]any {
	return mergeMaps(map[string]any{}, overrides)
}

func buildIstiodValues(p *OrbitPlatform) map[string]any {
	return mergeMaps(map[string]any{}, p.Spec.IstiodValues)
}

func buildGatewayValues(p *OrbitPlatform) map[string]any {
	return mergeMaps(map[string]any{}, p.Spec.GatewayValues)
}

func buildOrbitPlatformValues(defaults map[string]any, trustDomain string, tenantNamespaces []string, overrides map[string]any) map[string]any {
	values := mergeMaps(defaults, map[string]any{
		"global": map[string]any{
			"trustDomain": trustDomain,
		},
		"namespaces": map[string]any{
			"system":  "orbit-system",
			"tenants": tenantNamespaces,
		},
	})

	return mergeMaps(values, overrides)
}

func buildOrbitValues(defaults map[string]any, p *OrbitPlatform, secretName string) map[string]any {
	values := mergeMaps(defaults, map[string]any{
		"global": map[string]any{
			"trustDomain": p.Spec.TrustDomain,
		},
		"identity": map[string]any{
			"existingSecret":              secretName,
			"existingConfigMap":           "orbit-config",
			"existingTenantsConfigMap":    "orbit-tenants",
			"existingClientSecretsSecret": "orbit-tenant-oidc-secrets",
		},
		"platform": map[string]any{
			"namespaces": map[string]any{
				"system": "orbit-system",
			},
			"spiffe": map[string]any{
				"enabled": p.Spec.SPIRE.Enabled,
			},
		},
	})

	if p.Spec.Config.DB.Host != "" {
		values = mergeMaps(values, map[string]any{
			"config": map[string]any{
				"db": map[string]any{
					"host": p.Spec.Config.DB.Host,
					"name": p.Spec.Config.DB.Name,
					"port": p.Spec.Config.DB.Port,
					"user": p.Spec.Config.DB.User,
				},
			},
		})
	}
	if p.Spec.Config.Redis.Host != "" {
		values = mergeMaps(values, map[string]any{
			"config": map[string]any{
				"redis": map[string]any{
					"host": p.Spec.Config.Redis.Host,
					"port": p.Spec.Config.Redis.Port,
				},
			},
		})
	}

	return mergeMaps(values, p.Spec.OrbitValues)
}
