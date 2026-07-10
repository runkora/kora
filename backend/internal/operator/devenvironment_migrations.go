// Database migrations for a devenvironment's fixture Postgres (closing the
// "orbit-identity cannot boot against an unmigrated schema" gap tracked in
// docs/adr/2026-07-07-ephemeral-development-environments-via-vcluster.md).
//
// A Kubernetes Job inside the vcluster — not the reconciler speaking the
// Postgres wire protocol itself — per the ADR's own recommendation. The Job
// runs the OPERATOR'S OWN IMAGE: Dockerfile.operator bakes the goose binary
// and every per-service Goose migration (flattened, mirroring the Makefile's
// COLLECT_MIGRATIONS) into /etc/orbit/migrations, so the migrations applied
// always match the operator build exactly, with no external migration image
// to version, trust, or mirror for air-gapped registries.
package operator

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// inVClusterMigrationsJobName — one Job per environment lifetime, in the
	// vcluster's orbit-system namespace (where the orbit-secrets Secret it
	// reads DB_PASSWORD from lives). Jobs are immutable: once created it is
	// left alone, complete or still retrying. An environment provisioned
	// before a migration landed does not re-migrate — dev environments are
	// TTL-bounded and disposable; provision a fresh one instead.
	inVClusterMigrationsJobName = "orbit-db-migrate"

	// inVClusterMigrationsPath is where Dockerfile.operator bakes the
	// collected .sql files.
	inVClusterMigrationsPath = "/etc/orbit/migrations"
)

// ensureInVClusterMigrations creates the migration Job once the fixtures are
// ensured. It does not wait for completion: goose fails and retries (Job
// backoff) until the fixture Postgres accepts connections, and the
// in-vcluster orbit-identity crash-loops harmlessly until the schema exists —
// standard Kubernetes convergence, no ordering logic needed here.
func (r *DevEnvironmentReconciler) ensureInVClusterMigrations(ctx context.Context, vcClient client.Client) error {
	existing := &batchv1.Job{}
	err := vcClient.Get(ctx, client.ObjectKey{Name: inVClusterMigrationsJobName, Namespace: "orbit-system"}, existing)
	switch {
	case err == nil:
		return nil
	case !apierrors.IsNotFound(err):
		return fmt.Errorf("get migration job %q: %w", inVClusterMigrationsJobName, err)
	}

	backoffLimit := int32(20)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      inVClusterMigrationsJobName,
			Namespace: "orbit-system",
			Labels:    fixtureLabels(inVClusterMigrationsJobName),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: fixtureLabels(inVClusterMigrationsJobName),
					// orbit-system is injection-labeled once the in-vcluster
					// operator reconciles it; an injected sidecar would keep a
					// Job pod running forever after goose exits.
					Annotations: map[string]string{"sidecar.istio.io/inject": "false"},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:      "goose",
							Image:     r.inVClusterOperatorImage(),
							Command:   []string{"goose", "-dir", inVClusterMigrationsPath, "up"},
							Resources: fixtureResources(),
							Env: []corev1.EnvVar{
								{
									Name: "DB_PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "orbit-secrets"},
											Key:                  "DB_PASSWORD",
										},
									},
								},
								{Name: "GOOSE_DRIVER", Value: "postgres"},
								{
									// DB_PASSWORD is RawURLEncoding base64
									// (randomToken) — URL-safe by construction,
									// so plain $(VAR) interpolation into the DSN
									// is well-formed.
									Name: "GOOSE_DBSTRING",
									Value: fmt.Sprintf("postgres://%s:$(DB_PASSWORD)@%s:%d/%s?sslmode=disable",
										inVClusterDBUser, inVClusterPostgresHost(), inVClusterPostgresPort, inVClusterDBName),
								},
							},
						},
					},
				},
			},
		},
	}
	if createErr := vcClient.Create(ctx, job); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
		return fmt.Errorf("create migration job %q: %w", inVClusterMigrationsJobName, createErr)
	}
	return nil
}

// inVClusterOperatorImage is the operator's own image reference as used
// inside a vcluster — the same registry/repo:tag convention
// inVClusterOperatorValues configures for the in-vcluster operator chart.
func (r *DevEnvironmentReconciler) inVClusterOperatorImage() string {
	repo := "orbit-operator"
	if r.ImageRegistry != "" {
		repo = r.ImageRegistry + "/orbit-operator"
	}
	return repo + ":latest"
}
