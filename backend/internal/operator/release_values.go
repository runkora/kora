package operator

func buildSPIRECRDsValues(overrides map[string]any) map[string]any {
	return mergeMaps(map[string]any{}, overrides)
}

func buildSPIREValues(p *OrbitPlatform) map[string]any {
	return mergeMaps(map[string]any{}, p.Spec.SPIRE.HelmValues)
}

func buildIstioBaseValues(p *OrbitPlatform) map[string]any {
	return mergeMaps(map[string]any{}, p.Spec.Istio.HelmValues)
}

func buildIstiodValues(p *OrbitPlatform) map[string]any {
	return mergeMaps(map[string]any{}, p.Spec.Istiod.HelmValues)
}

func buildGatewayValues(p *OrbitPlatform) map[string]any {
	return mergeMaps(map[string]any{}, p.Spec.IstioGateway.HelmValues)
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
	configMapName := p.Spec.platformConfigMapName()

	values := mergeMaps(defaults, map[string]any{
		"global": map[string]any{
			"trustDomain": p.Spec.TrustDomain,
		},
		"identity": map[string]any{
			"existingSecret":              secretName,
			"existingConfigMap":           configMapName,
			"existingTenantsConfigMap":    "orbit-tenants",
			"existingClientSecretsSecret": "orbit-tenant-oidc-secrets",
		},
		"inferenceGateway": map[string]any{
			"existingConfigMap": configMapName,
		},
		"platform": map[string]any{
			"namespaces": map[string]any{
				"system": "orbit-system",
			},
			"spiffe": map[string]any{
				"enabled": p.Spec.SPIRE.Enabled,
			},
		},
		"policyService": map[string]any{
			"existingConfigMap": configMapName,
		},
	})

	if p.Spec.Config.IsEnabled() && p.Spec.Config.DB.IsEnabled() && p.Spec.Config.DB.HasValues() {
		values = mergeMaps(values, map[string]any{
			"config": map[string]any{
				"db": map[string]any{
					"host": p.Spec.Config.DB.Host,
					"name": p.Spec.Config.DB.Name,
					"port": p.Spec.Config.DB.PortOrDefault(),
					"user": p.Spec.Config.DB.User,
				},
			},
		})
	}
	if p.Spec.Config.IsEnabled() && p.Spec.Config.Redis.IsEnabled() && p.Spec.Config.Redis.HasValues() {
		values = mergeMaps(values, map[string]any{
			"config": map[string]any{
				"redis": map[string]any{
					"host": p.Spec.Config.Redis.Host,
					"port": p.Spec.Config.Redis.PortOrDefault(),
				},
			},
		})
	}

	return mergeMaps(values, p.Spec.OrbitValues)
}
