package operator

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"testing"
)

func TestBuildSPIRECRDsValuesMergesOverrides(t *testing.T) {
	t.Parallel()

	got := buildSPIRECRDsValues(map[string]any{
		"rbac": map[string]any{
			"create": false,
		},
	})

	assertDeepEqual(t, false, nestedValue(t, got, "rbac", "create"))
}

func TestBuildSPIREValuesUsesCRValuesOnly(t *testing.T) {
	t.Parallel()

	p := &OrbitPlatform{
		Spec: OrbitPlatformSpec{
			SpireValues: map[string]any{
				"global": map[string]any{
					"spire": map[string]any{
						"clusterName": "orbit-local",
						"trustDomain": "orbit.test",
					},
				},
				"spire-agent": map[string]any{
					"sds": map[string]any{
						"defaultSVIDName": "default",
					},
				},
			},
		},
	}

	got := buildSPIREValues(p)

	assertDeepEqual(t, "orbit.test", nestedValue(t, got, "global", "spire", "trustDomain"))
	assertDeepEqual(t, "orbit-local", nestedValue(t, got, "global", "spire", "clusterName"))
	assertDeepEqual(t, "default", nestedValue(t, got, "spire-agent", "sds", "defaultSVIDName"))
}

func TestBuildIstioBaseValuesMergesOverrides(t *testing.T) {
	t.Parallel()

	got := buildIstioBaseValues(map[string]any{
		"defaultRevision": "stable",
	})

	assertDeepEqual(t, "stable", nestedValue(t, got, "defaultRevision"))
}

func TestBuildIstiodValuesUsesCRValuesOnly(t *testing.T) {
	t.Parallel()

	p := &OrbitPlatform{
		Spec: OrbitPlatformSpec{
			IstiodValues: map[string]any{
				"meshConfig": map[string]any{
					"trustDomain": "orbit.test",
					"defaultConfig": map[string]any{
						"discoveryAddress": "istiod.istio-system.svc:15012",
					},
				},
				"sidecarInjectorWebhook": map[string]any{
					"templates": map[string]any{
						"spire": "spire-template",
					},
				},
			},
		},
	}

	got := buildIstiodValues(p)

	assertDeepEqual(t, "orbit.test", nestedValue(t, got, "meshConfig", "trustDomain"))
	assertDeepEqual(t, "istiod.istio-system.svc:15012", nestedValue(t, got, "meshConfig", "defaultConfig", "discoveryAddress"))
	assertDeepEqual(t, "spire-template", nestedValue(t, got, "sidecarInjectorWebhook", "templates", "spire"))
}

func TestBuildGatewayValuesUsesCRValuesOnly(t *testing.T) {
	t.Parallel()

	p := &OrbitPlatform{
		Spec: OrbitPlatformSpec{
			GatewayValues: map[string]any{
				"service": map[string]any{
					"type": "NodePort",
				},
				"env": map[string]any{
					"PILOT_CERT_PROVIDER":    "spire",
					"SPIFFE_ENDPOINT_SOCKET": "unix:///run/secrets/workload-spiffe-uds/socket",
				},
			},
		},
	}

	got := buildGatewayValues(p)

	assertDeepEqual(t, "NodePort", nestedValue(t, got, "service", "type"))
	assertDeepEqual(t, "spire", nestedValue(t, got, "env", "PILOT_CERT_PROVIDER"))
	assertDeepEqual(t, "unix:///run/secrets/workload-spiffe-uds/socket", nestedValue(t, got, "env", "SPIFFE_ENDPOINT_SOCKET"))
}

func TestBuildOrbitPlatformValuesPrecedence(t *testing.T) {
	t.Parallel()

	defaults := map[string]any{
		"global": map[string]any{
			"trustDomain": "default.test",
		},
		"namespaces": map[string]any{
			"system":  "default-system",
			"tenants": []string{"default-tenant"},
		},
		"networkPolicy": map[string]any{
			"enabled": false,
		},
	}

	got := buildOrbitPlatformValues(defaults, "computed.test", []string{"tenant-a"}, map[string]any{
		"global": map[string]any{
			"trustDomain": "override.test",
		},
		"networkPolicy": map[string]any{
			"enabled": true,
		},
	})

	assertDeepEqual(t, "override.test", nestedValue(t, got, "global", "trustDomain"))
	assertDeepEqual(t, "orbit-system", nestedValue(t, got, "namespaces", "system"))
	assertDeepEqual(t, []string{"tenant-a"}, nestedValue(t, got, "namespaces", "tenants"))
	assertDeepEqual(t, true, nestedValue(t, got, "networkPolicy", "enabled"))
}

func TestBuildOrbitValuesPrecedenceAndExistingBehavior(t *testing.T) {
	t.Parallel()

	p := &OrbitPlatform{
		Spec: OrbitPlatformSpec{
			TrustDomain: "orbit.test",
			SPIRE:       SPIRESpec{Enabled: true},
			Config: PlatformConfigSpec{
				DB: DBConfig{
					Host: "db.orbit.local",
					Name: "orbit",
					Port: 5432,
					User: "orbit",
				},
				Redis: RedisConfig{
					Host: "redis.orbit.local",
					Port: 6379,
				},
			},
			OrbitValues: map[string]any{
				"identity": map[string]any{
					"existingSecret": "override-secret",
				},
				"config": map[string]any{
					"db": map[string]any{
						"host": "override-db",
					},
				},
			},
		},
	}

	defaults := map[string]any{
		"identity": map[string]any{
			"existingSecret": "default-secret",
		},
		"extra": map[string]any{
			"preserved": true,
		},
	}

	got := buildOrbitValues(defaults, p, "computed-secret")

	assertDeepEqual(t, "orbit.test", nestedValue(t, got, "global", "trustDomain"))
	assertDeepEqual(t, "override-secret", nestedValue(t, got, "identity", "existingSecret"))
	assertDeepEqual(t, "orbit-config", nestedValue(t, got, "identity", "existingConfigMap"))
	assertDeepEqual(t, "override-db", nestedValue(t, got, "config", "db", "host"))
	assertDeepEqual(t, 6379, nestedValue(t, got, "config", "redis", "port"))
	assertDeepEqual(t, true, nestedValue(t, got, "platform", "spiffe", "enabled"))
	assertDeepEqual(t, true, nestedValue(t, got, "extra", "preserved"))
}

func TestOrbitPlatformSpecDeepCopyIntoCopiesRawValueMaps(t *testing.T) {
	t.Parallel()

	original := OrbitPlatformSpec{
		SPIRECRDsValues: map[string]any{
			"labels": []string{"a", "b"},
		},
		SpireValues: map[string]any{
			"spire-agent": map[string]any{
				"sds": map[string]any{
					"defaultBundleName": "ROOTCA",
				},
			},
		},
		IstioBaseValues: map[string]any{
			"revisions": []any{"default"},
		},
		IstiodValues: map[string]any{
			"meshConfig": map[string]any{
				"defaultConfig": map[string]any{
					"proxyMetadata": map[string]any{
						"key": "value",
					},
				},
			},
		},
		GatewayValues: map[string]any{
			"service": map[string]any{
				"ports": []any{80, 443},
			},
		},
		PlatformValues: map[string]any{
			"networkPolicy": map[string]any{
				"cidrs": []string{"10.0.0.0/24"},
			},
		},
		OrbitValues: map[string]any{
			"identity": map[string]any{
				"hostAliases": []map[string]any{
					{"ip": "1.2.3.4"},
				},
			},
		},
	}

	var copied OrbitPlatformSpec
	original.DeepCopyInto(&copied)

	original.SPIRECRDsValues["labels"].([]string)[0] = "changed"
	original.SpireValues["spire-agent"].(map[string]any)["sds"].(map[string]any)["defaultBundleName"] = "changed"
	original.IstioBaseValues["revisions"].([]any)[0] = "changed"
	original.IstiodValues["meshConfig"].(map[string]any)["defaultConfig"].(map[string]any)["proxyMetadata"].(map[string]any)["key"] = "changed"
	original.GatewayValues["service"].(map[string]any)["ports"].([]any)[0] = 8080
	original.PlatformValues["networkPolicy"].(map[string]any)["cidrs"].([]string)[0] = "changed"
	original.OrbitValues["identity"].(map[string]any)["hostAliases"].([]map[string]any)[0]["ip"] = "9.9.9.9"

	assertDeepEqual(t, []string{"a", "b"}, copied.SPIRECRDsValues["labels"])
	assertDeepEqual(t, "ROOTCA", nestedValue(t, copied.SpireValues, "spire-agent", "sds", "defaultBundleName"))
	assertDeepEqual(t, []any{"default"}, copied.IstioBaseValues["revisions"])
	assertDeepEqual(t, "value", nestedValue(t, copied.IstiodValues, "meshConfig", "defaultConfig", "proxyMetadata", "key"))
	assertDeepEqual(t, []any{80, 443}, nestedValue(t, copied.GatewayValues, "service", "ports"))
	assertDeepEqual(t, []string{"10.0.0.0/24"}, nestedValue(t, copied.PlatformValues, "networkPolicy", "cidrs"))
	assertDeepEqual(t, "1.2.3.4", nestedValue(t, copied.OrbitValues, "identity", "hostAliases", 0, "ip"))
}

func TestOrbitPlatformCRDTemplateIncludesReleaseValueOverrideFields(t *testing.T) {
	t.Parallel()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "charts", "orbit-operator", "templates", "crd-orbitplatform.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CRD template: %v", err)
	}

	content := string(data)
	fields := []string{
		"spireCRDsValues",
		"spireValues",
		"istioBaseValues",
		"istiodValues",
		"gatewayValues",
		"platformValues",
		"orbitValues",
	}

	for _, field := range fields {
		pattern := regexp.MustCompile(`(?s)` + field + `:\n.*?x-kubernetes-preserve-unknown-fields: true`)
		if !pattern.MatchString(content) {
			t.Fatalf("expected preserve-unknown-fields block for %q", field)
		}
	}
}

func assertDeepEqual(t *testing.T, want, got any) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("unexpected value\nwant: %#v\ngot:  %#v", want, got)
	}
}

func nestedValue(t *testing.T, value any, path ...any) any {
	t.Helper()

	current := value
	for _, segment := range path {
		switch key := segment.(type) {
		case string:
			asMap, ok := current.(map[string]any)
			if !ok {
				t.Fatalf("value at %v is %T, want map[string]any", path, current)
			}
			next, ok := asMap[key]
			if !ok {
				t.Fatalf("missing key %q in path %v", key, path)
			}
			current = next
		case int:
			switch items := current.(type) {
			case []any:
				current = items[key]
			case []map[string]any:
				current = items[key]
			default:
				t.Fatalf("value at %v is %T, want slice", path, current)
			}
		default:
			t.Fatalf("unsupported path segment type %T", segment)
		}
	}

	return current
}
