// In-vcluster Postgres/Redis/Dex/glauth fixtures for OrbitDevEnvironment
// (decision 2 of docs/adr/2026-07-07-ephemeral-development-environments-via-vcluster.md:
// "Postgres, Redis, and Dex run inside the vcluster with fixture data;
// environments never share the host control plane's database or Redis").
//
// These are deliberately minimal, hand-built Deployments/Services rather than
// a Helm chart, but Dex is backed by the same LDAP-via-glauth wiring Tier 1/2
// local dev uses (config/dex/config.yaml + config/glauth/config.cfg) rather
// than Dex's own staticPasswords/enablePasswordDB connector — the latter has
// no concept of group membership, and every scope a devenvironment principal
// resolves comes from the OIDC "groups" claim (DefaultRoleScopes,
// policyservice/bundle/compiler.go), so a passwordDB-only Dex would leave
// every devenvironment bootstrap user permanently scope-less (confirmed
// live: Triggers, Policy, and most of Model Gateway hidden from the sidebar
// for lack of scopes). Ephemeral (emptyDir) storage throughout — these are
// disposable fixtures, not production databases/directories.
package operator

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// inVClusterFixturesNamespace is dedicated to Postgres/Redis/Dex and
	// managed directly by this reconciler — deliberately NOT "orbit-system",
	// which the in-vcluster orbit-operator owns and only creates once it
	// starts reconciling the OrbitPlatform CR. Keeping fixtures in their own
	// namespace, created eagerly by us, avoids an ordering dependency on the
	// in-vcluster operator's own bootstrap sequence.
	inVClusterFixturesNamespace = "orbit-devenv-fixtures"

	inVClusterPostgresName  = "orbit-postgres"
	inVClusterPostgresImage = "postgres:17-alpine"
	inVClusterPostgresPort  = 5432
	inVClusterDBName        = "orbit"
	inVClusterDBUser        = "orbit"

	inVClusterRedisName  = "orbit-redis"
	inVClusterRedisImage = "redis:7-alpine"
	inVClusterRedisPort  = 6379

	inVClusterDexName       = "orbit-dex"
	inVClusterDexImage      = "dexidp/dex:v2.41.1"
	inVClusterDexPort       = 5556
	inVClusterDexConfigFile = "config.yaml"

	// inVClusterGlauthName is the lightweight LDAP directory (matching Tier
	// 1/2 local dev's own glauth, config/glauth/config.cfg) backing Dex's LDAP
	// connector — Dex's built-in staticPasswords/enablePasswordDB connector
	// cannot return group membership, and DefaultRoleScopes
	// (policyservice/bundle/compiler.go) is keyed entirely off the "groups"
	// OIDC claim. Without this, every devenvironment's bootstrap user
	// resolves zero scopes and the frontend hides every scope-gated nav item
	// (Triggers, Policy, most of Model Gateway) — confirmed live.
	inVClusterGlauthName       = "orbit-glauth"
	inVClusterGlauthImage      = "ghcr.io/glauth/glauth:v2.3.2"
	inVClusterGlauthPort       = 3893
	inVClusterGlauthConfigFile = "config.cfg"

	// inVClusterDevUserEmail/Password are the fixed devenv login credentials,
	// matching Tier 1/2 local dev's own test user (config/dex/config.yaml) so
	// the same muscle memory ("dev@orbit.local.clearroute.io / password")
	// works in a dev environment too.
	inVClusterDevUserEmail       = "dev@orbit.local.clearroute.io"
	inVClusterDevUserEmailDomain = "orbit.local.clearroute.io"
	inVClusterOIDCClientID       = "orbit-local"

	// inVClusterFixturePasswordSHA256 is the SHA256 hash of "password" —
	// glauth's passsha256 field requires the raw hex digest (config/glauth's
	// own passsha256 uses the same fixed dev password, hashed at container
	// startup instead of hardcoded). Fixture-only, never real credentials.
	inVClusterFixturePasswordSHA256 = "5e884898da28047151d0e56f8dc6292773603d0d6aabbdd62a11ef721d1542d8"

	// inVClusterSigningKeyBits is deliberately smaller than the 4096-bit key
	// `make gen-signing-key` writes for real installs — this key protects
	// nothing but a disposable environment's own throwaway tokens, and
	// generating one per environment provision should stay fast.
	inVClusterSigningKeyBits = 2048
)

// inVClusterSecretValues holds the credentials shared between the
// orbit-secrets Secret (read by the in-vcluster orbit-identity service) and
// the Postgres/Redis fixtures themselves — both sides must agree, so they are
// generated together once and read back unchanged on every later reconcile.
type inVClusterSecretValues struct {
	DBPassword    string
	RedisPassword string
	SigningKeyPEM []byte
}

// ensureInVClusterSecrets creates the orbit-system namespace and orbit-secrets
// Secret the in-vcluster orbit-operator's PlatformReconciler reads (and never
// creates itself — see PlatformReconciler's own comment on
// PlatformSecretsSpec), generating fresh devenv-only credentials on first
// create and leaving them untouched on subsequent reconciles so they stay in
// sync with whatever the fixtures were actually deployed with.
func ensureInVClusterSecrets(ctx context.Context, vcClient client.Client) (*inVClusterSecretValues, error) {
	const namespace = "orbit-system"
	const name = "orbit-secrets"

	// This runs before the in-vcluster orbit-operator's own PlatformReconciler
	// ever gets scheduled, so it wins the race to create orbit-system — which
	// means it must carry the same istio-injection/spiffe labels
	// ensureSystemNamespace (platform_controller.go) and the orbit-platform
	// chart's namespaces.yaml apply on a host install, or their own
	// lookup/already-exists guards silently no-op and no orbit-system pod in
	// the devenvironment ever gets a sidecar injected (confirmed live: every
	// chart-shipped service came up as a single, un-injected container).
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: namespace,
		Labels: map[string]string{
			"istio-injection":         "enabled",
			"spiffe.io/spire-managed": "true",
		},
	}}
	if err := vcClient.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create namespace %q: %w", namespace, err)
	}

	existing := &corev1.Secret{}
	err := vcClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, existing)
	switch {
	case apierrors.IsNotFound(err):
		values, genErr := generateInVClusterSecretValues()
		if genErr != nil {
			return nil, fmt.Errorf("generate devenv platform secret values: %w", genErr)
		}
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			// Data (not StringData) directly: StringData is an API-server-side
			// convenience that gets merged into Data at admission time — the
			// fake client used in tests does not perform that merge, and
			// writing Data directly works identically against a real cluster.
			Data: map[string][]byte{
				"DB_PASSWORD":             []byte(values.DBPassword),
				"REDIS_PASSWORD":          []byte(values.RedisPassword),
				"ORBIT_TOKEN_SIGNING_KEY": values.SigningKeyPEM,
			},
		}
		if createErr := vcClient.Create(ctx, secret); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return nil, fmt.Errorf("create secret %q: %w", name, createErr)
		}
		return values, nil
	case err != nil:
		return nil, fmt.Errorf("get secret %q: %w", name, err)
	default:
		return &inVClusterSecretValues{
			DBPassword:    string(existing.Data["DB_PASSWORD"]),
			RedisPassword: string(existing.Data["REDIS_PASSWORD"]),
			SigningKeyPEM: existing.Data["ORBIT_TOKEN_SIGNING_KEY"],
		}, nil
	}
}

// ensureInVClusterFrontendSecret creates the orbit-frontend-secrets Secret
// the orbit chart's frontend deployment references as an existingSecret
// (charts/orbit/values.yaml: frontend.existingSecret). On a host install this
// is provisioned out-of-band by scripts/cluster/create-secrets.sh, never by
// the operator — but a devenvironment has no equivalent out-of-band step, so
// without this the frontend pod never leaves CreateContainerConfigError
// (confirmed live: "secret \"orbit-frontend-secrets...\" not found"). A fresh
// throwaway value on first create only, left untouched after — same idiom as
// ensureInVClusterSecrets.
func ensureInVClusterFrontendSecret(ctx context.Context, vcClient client.Client) error {
	const namespace = "orbit-system"
	const name = "orbit-frontend-secrets"

	existing := &corev1.Secret{}
	err := vcClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, existing)
	switch {
	case apierrors.IsNotFound(err):
		// 32 random bytes, base64url-encoded, is 43 characters — comfortably
		// past create-secrets.sh's own 32-character minimum for this value.
		sessionSecret, genErr := randomToken(32)
		if genErr != nil {
			return fmt.Errorf("generate BFF session secret: %w", genErr)
		}
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Data:       map[string][]byte{"BFF_SESSION_SECRET": []byte(sessionSecret)},
		}
		if createErr := vcClient.Create(ctx, secret); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("create secret %q: %w", name, createErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("get secret %q: %w", name, err)
	default:
		return nil
	}
}

func generateInVClusterSecretValues() (*inVClusterSecretValues, error) {
	dbPassword, err := randomToken(24)
	if err != nil {
		return nil, err
	}
	redisPassword, err := randomToken(24)
	if err != nil {
		return nil, err
	}
	signingKeyPEM, err := generateRSASigningKeyPEM()
	if err != nil {
		return nil, err
	}
	return &inVClusterSecretValues{
		DBPassword:    dbPassword,
		RedisPassword: redisPassword,
		SigningKeyPEM: signingKeyPEM,
	}, nil
}

func randomToken(numBytes int) (string, error) {
	buf := make([]byte, numBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// generateRSASigningKeyPEM generates a throwaway RSA private key in the same
// PKCS#1 PEM format `make gen-signing-key` writes to secrets/signing.key, so
// the in-vcluster orbit-identity service reads ORBIT_TOKEN_SIGNING_KEY
// exactly as it would in any other environment.
func generateRSASigningKeyPEM() ([]byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, inVClusterSigningKeyBits)
	if err != nil {
		return nil, fmt.Errorf("generate RSA key: %w", err)
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}), nil
}

// inVClusterPostgresHost / inVClusterRedisHost are the cross-namespace
// in-vcluster DNS names the OrbitPlatform CR references —
// "{service}.{namespace}" resolves from any namespace via the default
// "svc.cluster.local" search suffix, the same short form confirmed
// empirically for the host-to-vcluster hop in devenvironment_platform.go.
// Dex has no equivalent internal-DNS helper: its issuer must be the
// browser-reachable external URL (appDexIssuerURL,
// devenvironment_app_ingress.go), threaded in below as externalIssuerURL —
// using the in-vcluster DNS name here was the previously-tracked gap that
// made browser login impossible (see appDexIssuerURL's doc comment).
func inVClusterPostgresHost() string {
	return fmt.Sprintf("%s.%s", inVClusterPostgresName, inVClusterFixturesNamespace)
}

func inVClusterRedisHost() string {
	return fmt.Sprintf("%s.%s", inVClusterRedisName, inVClusterFixturesNamespace)
}

func inVClusterGlauthHost() string {
	return fmt.Sprintf("%s.%s", inVClusterGlauthName, inVClusterFixturesNamespace)
}

// ensureFixtures deploys Postgres, Redis, and Dex into
// inVClusterFixturesNamespace, configured with the credentials from
// ensureInVClusterSecrets so the in-vcluster orbit-identity service (which
// reads those same credentials from orbit-secrets) can actually connect.
// externalIssuerURL is Dex's browser- and identity-service-reachable issuer
// (appDexIssuerURL) — it must match the OrbitTenant's own configured OIDC
// issuer exactly (devenvironment_platform.go), or discovery's issuer check
// fails.
func ensureFixtures(ctx context.Context, vcClient client.Client, secrets *inVClusterSecretValues, externalCallbackURL, externalIssuerURL string) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: inVClusterFixturesNamespace}}
	if err := vcClient.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %q: %w", inVClusterFixturesNamespace, err)
	}

	if err := ensureDeployment(ctx, vcClient, postgresDeployment(secrets.DBPassword)); err != nil {
		return fmt.Errorf("ensure postgres deployment: %w", err)
	}
	if err := ensureService(ctx, vcClient, postgresService()); err != nil {
		return fmt.Errorf("ensure postgres service: %w", err)
	}

	if err := ensureDeployment(ctx, vcClient, redisDeployment(secrets.RedisPassword)); err != nil {
		return fmt.Errorf("ensure redis deployment: %w", err)
	}
	if err := ensureService(ctx, vcClient, redisService()); err != nil {
		return fmt.Errorf("ensure redis service: %w", err)
	}

	if err := ensureConfigMap(ctx, vcClient, glauthConfigMap()); err != nil {
		return fmt.Errorf("ensure glauth configmap: %w", err)
	}
	if err := ensureDeployment(ctx, vcClient, glauthDeployment()); err != nil {
		return fmt.Errorf("ensure glauth deployment: %w", err)
	}
	if err := ensureService(ctx, vcClient, glauthService()); err != nil {
		return fmt.Errorf("ensure glauth service: %w", err)
	}

	if err := ensureConfigMap(ctx, vcClient, dexConfigMap(externalCallbackURL, externalIssuerURL)); err != nil {
		return fmt.Errorf("ensure dex configmap: %w", err)
	}
	if err := ensureDeployment(ctx, vcClient, dexDeployment()); err != nil {
		return fmt.Errorf("ensure dex deployment: %w", err)
	}
	if err := ensureService(ctx, vcClient, dexService()); err != nil {
		return fmt.Errorf("ensure dex service: %w", err)
	}
	return nil
}

// ensureConfigMap creates a ConfigMap if absent, or patches its Data if it
// already exists — the same idempotent-apply idiom as ensureDeployment.
func ensureConfigMap(ctx context.Context, c client.Client, desired *corev1.ConfigMap) error {
	existing := &corev1.ConfigMap{}
	err := c.Get(ctx, client.ObjectKey{Name: desired.Name, Namespace: desired.Namespace}, existing)
	switch {
	case apierrors.IsNotFound(err):
		if createErr := c.Create(ctx, desired); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("create configmap %q: %w", desired.Name, createErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("get configmap %q: %w", desired.Name, err)
	default:
		patch := client.MergeFrom(existing.DeepCopy())
		existing.Data = desired.Data
		if patchErr := c.Patch(ctx, existing, patch); patchErr != nil {
			return fmt.Errorf("patch configmap %q: %w", desired.Name, patchErr)
		}
		return nil
	}
}

// ensureDeployment creates a Deployment if absent, or patches its spec if it
// already exists — idempotent apply, matching the pattern used throughout
// devenvironment_controller.go for namespace/quota/limit-range reconciliation.
func ensureDeployment(ctx context.Context, c client.Client, desired *appsv1.Deployment) error {
	existing := &appsv1.Deployment{}
	err := c.Get(ctx, client.ObjectKey{Name: desired.Name, Namespace: desired.Namespace}, existing)
	switch {
	case apierrors.IsNotFound(err):
		if createErr := c.Create(ctx, desired); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("create deployment %q: %w", desired.Name, createErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("get deployment %q: %w", desired.Name, err)
	default:
		patch := client.MergeFrom(existing.DeepCopy())
		existing.Spec = desired.Spec
		if patchErr := c.Patch(ctx, existing, patch); patchErr != nil {
			return fmt.Errorf("patch deployment %q: %w", desired.Name, patchErr)
		}
		return nil
	}
}

func ensureService(ctx context.Context, c client.Client, desired *corev1.Service) error {
	existing := &corev1.Service{}
	err := c.Get(ctx, client.ObjectKey{Name: desired.Name, Namespace: desired.Namespace}, existing)
	switch {
	case apierrors.IsNotFound(err):
		if createErr := c.Create(ctx, desired); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("create service %q: %w", desired.Name, createErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("get service %q: %w", desired.Name, err)
	default:
		// ClusterIP is immutable once assigned — only the port/selector need
		// to converge, so patch spec fields individually rather than
		// overwriting the whole (existing) spec.
		patch := client.MergeFrom(existing.DeepCopy())
		existing.Spec.Ports = desired.Spec.Ports
		existing.Spec.Selector = desired.Spec.Selector
		if patchErr := c.Patch(ctx, existing, patch); patchErr != nil {
			return fmt.Errorf("patch service %q: %w", desired.Name, patchErr)
		}
		return nil
	}
}

func fixtureLabels(app string) map[string]string {
	return map[string]string{
		"app":                          app,
		"app.kubernetes.io/managed-by": "orbit-operator",
	}
}

// fixtureResources gives each fixture container an explicit, modest
// request/limit rather than falling through to the namespace LimitRange's
// default (sized for heavier third-party charts like istiod — see
// devEnvDefaultContainerMemoryLimit) — these are disposable fixtures, not
// production databases, and claiming the larger default per container would
// needlessly eat into the namespace's overall ResourceQuota headroom.
func fixtureResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("250m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}
}

func postgresDeployment(password string) *appsv1.Deployment {
	labels := fixtureLabels(inVClusterPostgresName)
	one := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: inVClusterPostgresName, Namespace: inVClusterFixturesNamespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &one,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": inVClusterPostgresName}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:      "postgres",
						Image:     inVClusterPostgresImage,
						Resources: fixtureResources(),
						Env: []corev1.EnvVar{
							{Name: "POSTGRES_USER", Value: inVClusterDBUser},
							{Name: "POSTGRES_PASSWORD", Value: password},
							{Name: "POSTGRES_DB", Value: inVClusterDBName},
							{Name: "PGDATA", Value: "/var/lib/postgresql/data/pgdata"},
						},
						Ports: []corev1.ContainerPort{{ContainerPort: inVClusterPostgresPort}},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "data", MountPath: "/var/lib/postgresql/data"},
						},
						ReadinessProbe: execProbe("pg_isready", "-U", inVClusterDBUser, "-d", inVClusterDBName),
					}},
					// Ephemeral storage — environments are disposable, and
					// there is no expectation data survives sleep/wake or
					// TTL expiry (decision 8 already scales this Deployment
					// to zero on sleep; PVCs would be pure overhead here).
					Volumes: []corev1.Volume{
						{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					},
				},
			},
		},
	}
}

func postgresService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: inVClusterPostgresName, Namespace: inVClusterFixturesNamespace},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": inVClusterPostgresName},
			Ports: []corev1.ServicePort{
				{Port: inVClusterPostgresPort, TargetPort: intstr.FromInt(inVClusterPostgresPort)},
			},
		},
	}
}

func redisDeployment(password string) *appsv1.Deployment {
	labels := fixtureLabels(inVClusterRedisName)
	one := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: inVClusterRedisName, Namespace: inVClusterFixturesNamespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &one,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": inVClusterRedisName}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:           "redis",
						Image:          inVClusterRedisImage,
						Resources:      fixtureResources(),
						Command:        []string{"redis-server", "--requirepass", password},
						Ports:          []corev1.ContainerPort{{ContainerPort: inVClusterRedisPort}},
						ReadinessProbe: execProbe("redis-cli", "-a", password, "--no-auth-warning", "ping"),
					}},
				},
			},
		},
	}
}

func redisService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: inVClusterRedisName, Namespace: inVClusterFixturesNamespace},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": inVClusterRedisName},
			Ports: []corev1.ServicePort{
				{Port: inVClusterRedisPort, TargetPort: intstr.FromInt(inVClusterRedisPort)},
			},
		},
	}
}

// dexConfig backs authentication with the glauth LDAP fixture (glauthConfig)
// rather than Dex's own staticPasswords/enablePasswordDB connector — Dex's
// built-in local connector has no concept of group membership, and every
// scope a devenvironment user resolves comes from the OIDC "groups" claim
// (DefaultRoleScopes, policyservice/bundle/compiler.go), so a passwordDB-only
// Dex leaves every devenvironment bootstrap user permanently scope-less. This
// mirrors Tier 1/2 local dev's own Dex-over-glauth wiring (config/dex/config.yaml)
// exactly, down to the LDAP search filters, so the same directory shape works
// unmodified inside a dev environment. In-memory storage — state is lost on
// restart, which is fine for a disposable environment.
//
// externalCallbackURL is the browser-facing PKCE redirect URI under the
// shared app-ingress host, path-prefixed with this environment's own ID
// ("{appExternalURL}/{env-id}/auth/callback", appAuthCallbackURL — see
// devenvironment_app_ingress.go) — must exactly match the redirect_uri the
// frontend sends (it reads its own current prefix client-side), or Dex
// rejects the token exchange with a redirect_uri mismatch. Registered
// alongside the relative device-flow callback.
//
// externalIssuerURL ({appExternalURL}/{env-id}/dex, appDexIssuerURL) is the
// issuer Dex is told to advertise and build every OIDC endpoint URL from
// (auth/token/keys/device) — a laptop browser can reach it through the same
// shared app-ingress gateway that serves the rest of the environment's UI,
// unlike the in-vcluster DNS name this used to be configured with.
func dexConfig(externalCallbackURL, externalIssuerURL string) string {
	return fmt.Sprintf(`issuer: %s
storage:
  type: memory
web:
  http: 0.0.0.0:%d
oauth2:
  deviceCodeGrant: true
  skipApprovalScreen: true
  responseTypes:
    - code
staticClients:
  - id: %q
    name: "Orbit (dev environment)"
    public: true
    redirectURIs:
      - "/device/callback"
      - %q
connectors:
  - type: ldap
    id: local-ldap
    name: "Email"
    config:
      host: %s:%d
      insecureNoSSL: true
      bindDN: cn=serviceuser,dc=orbit,dc=local
      bindPW: password
      userSearch:
        baseDN: dc=orbit,dc=local
        filter: "(objectClass=posixAccount)"
        username: mail
        idAttr: cn
        emailAttr: mail
        nameAttr: givenName
      groupSearch:
        baseDN: dc=orbit,dc=local
        filter: "(objectClass=posixGroup)"
        userMatchers:
          - userAttr: DN
            groupAttr: member
          - userAttr: cn
            groupAttr: memberUid
        nameAttr: cn
`, externalIssuerURL, inVClusterDexPort, inVClusterOIDCClientID, externalCallbackURL, inVClusterGlauthHost(), inVClusterGlauthPort)
}

func dexConfigMap(externalCallbackURL, externalIssuerURL string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: inVClusterDexName + "-config", Namespace: inVClusterFixturesNamespace},
		Data:       map[string]string{inVClusterDexConfigFile: dexConfig(externalCallbackURL, externalIssuerURL)},
	}
}

// glauthConfig is a minimal LDAP directory backing Dex's LDAP connector — a
// single bootstrap "dev" user in the "platform-admin" group (matching Tier
// 1/2's own dev@orbit.local.clearroute.io / password test user, config/glauth/config.cfg),
// so DefaultRoleScopes resolves the same full scope set inside a
// devenvironment as it does on any other install. Only one dev-facing user
// exists here — devenvironments are single-tenant, disposable, and have no
// equivalent to Tier 1/2's separate tenant-admin/engineer fixture accounts.
func glauthConfig() string {
	return fmt.Sprintf(`[ldap]
  enabled = true
  listen = "0.0.0.0:%d"

[ldaps]
  enabled = false

[backend]
  datastore = "config"
  baseDN = "dc=orbit,dc=local"

[[users]]
  name = "serviceuser"
  mail = "service@orbit.local.clearroute.io"
  uidnumber = 5000
  primarygroup = 5000
  passsha256 = %q

  [[users.capabilities]]
    action = "search"
    object = "*"

[[groups]]
  name = "svcaccts"
  gidnumber = 5000

[[users]]
  name = "dev"
  givenname = "Dev"
  sn = "User"
  mail = %q
  uidnumber = 1004
  primarygroup = 1100
  othergroups = [5001]
  passsha256 = %q

[[groups]]
  name = "users"
  gidnumber = 1100

[[groups]]
  name = "platform-admin"
  gidnumber = 5001
`, inVClusterGlauthPort, inVClusterFixturePasswordSHA256, inVClusterDevUserEmail, inVClusterFixturePasswordSHA256)
}

func glauthConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: inVClusterGlauthName + "-config", Namespace: inVClusterFixturesNamespace},
		Data:       map[string]string{inVClusterGlauthConfigFile: glauthConfig()},
	}
}

func glauthDeployment() *appsv1.Deployment {
	labels := fixtureLabels(inVClusterGlauthName)
	one := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: inVClusterGlauthName, Namespace: inVClusterFixturesNamespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &one,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": inVClusterGlauthName}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:      "glauth",
						Image:     inVClusterGlauthImage,
						Command:   []string{"/app/glauth", "-c", "/etc/glauth/" + inVClusterGlauthConfigFile},
						Resources: fixtureResources(),
						Ports:     []corev1.ContainerPort{{ContainerPort: inVClusterGlauthPort}},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "config", MountPath: "/etc/glauth"},
						},
					}},
					Volumes: []corev1.Volume{
						{Name: "config", VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: inVClusterGlauthName + "-config"},
							},
						}},
					},
				},
			},
		},
	}
}

func glauthService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: inVClusterGlauthName, Namespace: inVClusterFixturesNamespace},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": inVClusterGlauthName},
			Ports: []corev1.ServicePort{
				{Port: inVClusterGlauthPort, TargetPort: intstr.FromInt(inVClusterGlauthPort)},
			},
		},
	}
}

func dexDeployment() *appsv1.Deployment {
	labels := fixtureLabels(inVClusterDexName)
	one := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: inVClusterDexName, Namespace: inVClusterFixturesNamespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &one,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": inVClusterDexName}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:      "dex",
						Image:     inVClusterDexImage,
						Resources: fixtureResources(),
						Command:   []string{"dex", "serve", "/etc/dex/cfg/" + inVClusterDexConfigFile},
						Ports:     []corev1.ContainerPort{{ContainerPort: inVClusterDexPort}},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "config", MountPath: "/etc/dex/cfg"},
						},
					}},
					Volumes: []corev1.Volume{
						{Name: "config", VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: inVClusterDexName + "-config"},
							},
						}},
					},
				},
			},
		},
	}
}

func dexService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: inVClusterDexName, Namespace: inVClusterFixturesNamespace},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": inVClusterDexName},
			Ports: []corev1.ServicePort{
				{Port: inVClusterDexPort, TargetPort: intstr.FromInt(inVClusterDexPort)},
			},
		},
	}
}

func execProbe(command ...string) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{Command: command},
		},
		InitialDelaySeconds: 5,
		PeriodSeconds:       10,
	}
}
