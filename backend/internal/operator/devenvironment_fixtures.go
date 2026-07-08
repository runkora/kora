// In-vcluster Postgres/Redis/Dex fixtures for OrbitDevEnvironment (decision 2
// of docs/adr/2026-07-07-ephemeral-development-environments-via-vcluster.md:
// "Postgres, Redis, and Dex run inside the vcluster with fixture data;
// environments never share the host control plane's database or Redis").
//
// These are deliberately minimal, hand-built Deployments/Services rather than
// a Helm chart or the LDAP-backed Dex config used by Tier 1/2 local dev
// (config/dex/config.yaml + glauth): environments are disposable, so ephemeral
// (emptyDir) storage and a single static-password Dex user are enough to
// exercise the real install path (identity's OIDC discovery, Postgres/Redis
// connectivity) without standing up a second identity backend per environment.
//
// KNOWN GAP, not solved here: nothing runs database migrations against this
// Postgres instance. orbit-identity will start against an unmigrated schema
// until a migration step is added (tracked in the ADR's Next Steps) — this
// slice's scope is getting the in-vcluster OrbitPlatform CR to apply and
// begin reconciling, not full in-environment readiness.
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

	// inVClusterDevUserEmail/Password are the fixed devenv login credentials,
	// matching Tier 1/2 local dev's own test user (config/dex/config.yaml) so
	// the same muscle memory ("dev@orbit.local.clearroute.io / password")
	// works in a dev environment too. The bcrypt hash below is of "password"
	// (cost 10) — fixture-only, never real credentials.
	inVClusterDevUserEmail  = "dev@orbit.local.clearroute.io"
	inVClusterDevUserBcrypt = "$2a$10$uTiWKXmlM9vLJi2lISPv4.Tx1/1ugb1Xx7Pd1eZrMMha.0qseSza6"
	inVClusterDevUserID     = "08a8684b-db88-4b73-90a9-3cd1661f5466"
	inVClusterOIDCClientID  = "orbit-local"

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

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
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

// inVClusterPostgresHost / inVClusterRedisHost / inVClusterDexIssuerURL are
// the cross-namespace in-vcluster DNS names the OrbitPlatform/OrbitTenant CRs
// reference — "{service}.{namespace}" resolves from any namespace via the
// default "svc.cluster.local" search suffix, the same short form confirmed
// empirically for the host-to-vcluster hop in devenvironment_platform.go.
func inVClusterPostgresHost() string {
	return fmt.Sprintf("%s.%s", inVClusterPostgresName, inVClusterFixturesNamespace)
}

func inVClusterRedisHost() string {
	return fmt.Sprintf("%s.%s", inVClusterRedisName, inVClusterFixturesNamespace)
}

func inVClusterDexIssuerURLValue() string {
	return fmt.Sprintf("http://%s.%s:%d/dex", inVClusterDexName, inVClusterFixturesNamespace, inVClusterDexPort)
}

// ensureFixtures deploys Postgres, Redis, and Dex into
// inVClusterFixturesNamespace, configured with the credentials from
// ensureInVClusterSecrets so the in-vcluster orbit-identity service (which
// reads those same credentials from orbit-secrets) can actually connect.
func ensureFixtures(ctx context.Context, vcClient client.Client, secrets *inVClusterSecretValues) error {
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

	dexConfigMap := dexConfigMap()
	existingCM := &corev1.ConfigMap{}
	err := vcClient.Get(ctx, client.ObjectKey{Name: dexConfigMap.Name, Namespace: dexConfigMap.Namespace}, existingCM)
	switch {
	case apierrors.IsNotFound(err):
		if createErr := vcClient.Create(ctx, dexConfigMap); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("create dex configmap: %w", createErr)
		}
	case err != nil:
		return fmt.Errorf("get dex configmap: %w", err)
	default:
		patch := client.MergeFrom(existingCM.DeepCopy())
		existingCM.Data = dexConfigMap.Data
		if patchErr := vcClient.Patch(ctx, existingCM, patch); patchErr != nil {
			return fmt.Errorf("patch dex configmap: %w", patchErr)
		}
	}

	if err := ensureDeployment(ctx, vcClient, dexDeployment()); err != nil {
		return fmt.Errorf("ensure dex deployment: %w", err)
	}
	if err := ensureService(ctx, vcClient, dexService()); err != nil {
		return fmt.Errorf("ensure dex service: %w", err)
	}
	return nil
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

// dexConfig is a minimal, LDAP-free Dex config: a single static password
// user (matching Tier 1/2's own dev@orbit.local.clearroute.io / password
// test credentials) and the same orbit-local public client used everywhere
// else, so the same OIDC client wiring works unmodified inside a dev
// environment. In-memory storage — state is lost on restart, which is fine
// for a disposable environment.
func dexConfig() string {
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
enablePasswordDB: true
staticPasswords:
  - email: %q
    hash: %q
    username: dev
    userID: %q
staticClients:
  - id: %q
    name: "Orbit (dev environment)"
    public: true
    redirectURIs:
      - "/device/callback"
`, inVClusterDexIssuerURLValue(), inVClusterDexPort, inVClusterDevUserEmail, inVClusterDevUserBcrypt, inVClusterDevUserID, inVClusterOIDCClientID)
}

func dexConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: inVClusterDexName + "-config", Namespace: inVClusterFixturesNamespace},
		Data:       map[string]string{inVClusterDexConfigFile: dexConfig()},
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
