package devenvironments

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/orbitapi"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// kubeconfigPollInterval is how often Kubeconfig polls the CR's status for
// the operator's signature while bounded by KubeconfigConfig.SignTimeout.
const kubeconfigPollInterval = 500 * time.Millisecond

// kubeconfigClientCertOrg is the client certificate Organization the
// operator's signed certificate carries (parseAndVerifyCSR passes the CSR's
// Subject through verbatim — see internal/operator/devenvironment_csr.go).
// "system:masters" is the conventional Kubernetes admin identity: every
// stock apiserver (including the vcluster's own k3s-based one) binds it to
// cluster-admin via a built-in ClusterRoleBinding, which is exactly the
// "developer gets cluster-admin inside the vcluster" property decision 6
// requires — with no extra RBAC wiring needed inside each environment.
const kubeconfigClientCertOrg = "system:masters"

// Service implements the devenvironments business logic: registry CRUD,
// quota enforcement, and CR lifecycle actions. It never touches Postgres or
// the Kubernetes API directly — those are Repository and CRClient.
type Service struct {
	repo   Repository
	cr     CRClient
	cfg    *Config
	logger logging.Logger
}

// NewService constructs a Service.
func NewService(repo Repository, cr CRClient, cfg *Config, logger logging.Logger) *Service {
	return &Service{repo: repo, cr: cr, cfg: cfg, logger: logger}
}

// CreateEnvironment provisions a new environment for the caller's verified
// principal: a Postgres registry row, then the OrbitDevEnvironment CR the
// operator reconciles into a running vcluster.
func (s *Service) CreateEnvironment(ctx context.Context, req CreateRequest) (*Environment, error) {
	owner := ownerFromContext(ctx)
	if owner == "" {
		return nil, fmt.Errorf("%w: no authenticated principal in context", ErrValidation)
	}

	active, err := s.repo.CountActiveByOwner(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("count active dev environments: %w", err)
	}
	if active >= s.cfg.Quota.MaxActivePerOwner {
		return nil, ErrQuotaExceeded
	}

	id := generateEnvID()
	rec := &Record{ID: id, Owner: owner, Ref: req.Ref, CreatedAt: time.Now().UTC()}
	if err := s.repo.Create(ctx, rec); err != nil {
		return nil, fmt.Errorf("create dev environment registry row: %w", err)
	}

	cr := &orbitapi.OrbitDevEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: id},
		Spec: orbitapi.OrbitDevEnvironmentSpec{
			Owner: owner,
			Ref:   req.Ref,
			Awake: true,
		},
	}
	if err := s.cr.Create(ctx, cr); err != nil {
		// The registry row and the CR cannot be created atomically — roll the
		// row back so a failed create never leaves an orphaned, invisible
		// quota consumer behind.
		if delErr := s.repo.MarkDeleted(context.WithoutCancel(ctx), id); delErr != nil {
			s.logger.Warn(ctx, "failed to roll back registry row after CR create failure",
				logging.Field{Key: "id", Value: id},
				logging.Field{Key: "error", Value: delErr.Error()},
			)
		}
		return nil, fmt.Errorf("create OrbitDevEnvironment CR: %w", err)
	}

	return mergeEnvironment(rec, cr), nil
}

// GetEnvironment returns an environment by ID, visible only to its owner.
func (s *Service) GetEnvironment(ctx context.Context, id string) (*Environment, error) {
	rec, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ownerMatches(ctx, rec.Owner) {
		return nil, ErrNotFound
	}
	cr, err := s.cr.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// The CR is gone (TTL expiry, or an out-of-band kubectl delete) —
			// self-heal the registry rather than reporting a phantom row.
			_ = s.repo.MarkDeleted(ctx, id)
		}
		return nil, err
	}
	return mergeEnvironment(rec, cr), nil
}

// ListEnvironments returns every non-deleted environment owned by the
// caller's verified principal.
func (s *Service) ListEnvironments(ctx context.Context) ([]Environment, error) {
	owner := ownerFromContext(ctx)
	recs, err := s.repo.ListByOwner(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("list dev environments: %w", err)
	}
	return s.mergeList(ctx, recs)
}

// ListAllEnvironments returns every non-deleted environment regardless of
// owner. Callers must gate this behind the devenvironments:admin scope at
// the route level — the service performs no admin check itself.
func (s *Service) ListAllEnvironments(ctx context.Context) ([]Environment, error) {
	recs, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("list all dev environments: %w", err)
	}
	return s.mergeList(ctx, recs)
}

func (s *Service) mergeList(ctx context.Context, recs []Record) ([]Environment, error) {
	out := make([]Environment, 0, len(recs))
	for i := range recs {
		cr, err := s.cr.Get(ctx, recs[i].ID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				_ = s.repo.MarkDeleted(ctx, recs[i].ID)
				continue
			}
			return nil, err
		}
		out = append(out, *mergeEnvironment(&recs[i], cr))
	}
	return out, nil
}

// ExtendEnvironment raises an environment's TTL, up to the hard cap.
func (s *Service) ExtendEnvironment(ctx context.Context, id string, req ExtendRequest) (*Environment, error) {
	rec, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ownerMatches(ctx, rec.Owner) {
		return nil, ErrNotFound
	}

	ttl, err := time.ParseDuration(req.TTL)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid ttl %q: %v", ErrValidation, req.TTL, err) //nolint:errorlint // wrapping a sentinel plus a dynamic detail, not the error itself
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("%w: ttl must be positive", ErrValidation)
	}
	if ttl > ttlHardCap {
		return nil, ErrTTLExceedsHardCap
	}

	cr, err := s.cr.PatchSpec(ctx, id, func(spec *orbitapi.OrbitDevEnvironmentSpec) {
		spec.TTL = metav1.Duration{Duration: ttl}
	})
	if err != nil {
		return nil, fmt.Errorf("extend dev environment: %w", err)
	}
	return mergeEnvironment(rec, cr), nil
}

// WakeEnvironment flips an environment's desired power state back to awake
// (decision 8: wake is an explicit action, never transparent-on-connection).
// The reconciler restores the vcluster control plane and every in-vcluster
// workload's pre-sleep replica count on its next pass.
func (s *Service) WakeEnvironment(ctx context.Context, id string) (*Environment, error) {
	rec, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ownerMatches(ctx, rec.Owner) {
		return nil, ErrNotFound
	}

	cr, err := s.cr.PatchSpec(ctx, id, func(spec *orbitapi.OrbitDevEnvironmentSpec) {
		spec.Awake = true
	})
	if err != nil {
		return nil, fmt.Errorf("wake dev environment: %w", err)
	}
	return mergeEnvironment(rec, cr), nil
}

// DestroyEnvironment deletes an environment's CR — the operator's finalizer
// tears down the namespace and everything in it — and marks the registry
// row deleted.
func (s *Service) DestroyEnvironment(ctx context.Context, id string) error {
	rec, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if !ownerMatches(ctx, rec.Owner) {
		return ErrNotFound
	}
	if err := s.cr.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete OrbitDevEnvironment CR: %w", err)
	}
	return s.repo.MarkDeleted(ctx, id)
}

// Kubeconfig brokers a short-lived kubeconfig for the vcluster (decision 6):
// it generates a client keypair in-process, places the CSR on the CR, waits
// for the operator to sign it against the vcluster's own client CA, and
// assembles the kubeconfig. The private key is never persisted anywhere —
// it exists only for the duration of this call.
func (s *Service) Kubeconfig(ctx context.Context, id string) (*KubeconfigResponse, error) {
	rec, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ownerMatches(ctx, rec.Owner) {
		return nil, ErrNotFound
	}

	keyDER, csrPEM, err := generateKubeconfigCSR(rec.Owner)
	if err != nil {
		return nil, fmt.Errorf("generate kubeconfig csr: %w", err)
	}

	_, patchErr := s.cr.PatchSpec(ctx, id, func(spec *orbitapi.OrbitDevEnvironmentSpec) {
		spec.KubeconfigCSR = string(csrPEM)
	})
	if patchErr != nil {
		return nil, fmt.Errorf("submit kubeconfig csr: %w", patchErr)
	}

	fingerprint := orbitapi.CSRFingerprint(csrPEM)
	cr, err := s.waitForSignedCertificate(ctx, id, fingerprint)
	if err != nil {
		return nil, err
	}

	certPEM := []byte(cr.Status.KubeconfigCertificate)
	kubeconfigYAML, expiresAt, err := assembleKubeconfig(
		id, s.cfg.APIServer.ExternalAddress, cr.Status.IngressHost, certPEM, keyDER, []byte(cr.Status.KubeconfigCABundle))
	if err != nil {
		return nil, fmt.Errorf("assemble kubeconfig: %w", err)
	}

	return &KubeconfigResponse{Kubeconfig: kubeconfigYAML, ExpiresAt: expiresAt}, nil
}

// waitForSignedCertificate polls the CR until the operator has signed the
// CSR identified by fingerprint, or SignTimeout elapses.
func (s *Service) waitForSignedCertificate(ctx context.Context, id, fingerprint string) (*orbitapi.OrbitDevEnvironment, error) {
	deadline := time.Now().Add(s.cfg.Kubeconfig.SignTimeout())
	for {
		cr, err := s.cr.Get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("poll for signed kubeconfig: %w", err)
		}
		if cr.Status.KubeconfigCSRFingerprint == fingerprint && cr.Status.KubeconfigCertificate != "" {
			return cr, nil
		}
		if time.Now().After(deadline) {
			return nil, ErrKubeconfigSigningTimedOut
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(kubeconfigPollInterval):
		}
	}
}

// generateKubeconfigCSR generates a fresh ECDSA P-256 keypair and a
// PKCS#10 CSR requesting the given owner as the client certificate's
// identity. Returns the PKCS#8 DER-encoded private key (never leaves this
// process) and the PEM-encoded CSR (the only thing placed on the CR).
func generateKubeconfigCSR(owner string) (keyDER []byte, csrPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate client key: %w", err)
	}
	keyDER, err = x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal client key: %w", err)
	}

	template := &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: owner, Organization: []string{kubeconfigClientCertOrg}},
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create csr: %w", err)
	}
	csrPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
	return keyDER, csrPEM, nil
}

// assembleKubeconfig builds a complete, ready-to-use kubeconfig YAML document
// from the signed certificate and CA bundle.
//
// Resolves the ADR's "vcluster API server external-reachability question":
// server: points at apiServerExternalAddress — the shared Istio ingress
// gateway's TLS-passthrough listener (operator's devEnvAPIGatewayPort), a
// single address that does NOT need to resolve to this specific environment.
// Routing to the right environment is carried by TLSServerName (SNI) instead,
// set to ingressHost — since PASSTHROUGH mode never terminates TLS, the
// gateway picks a route purely from the ClientHello's SNI, and the client
// certificate reaches the vcluster's own apiserver untouched (see
// devenvironment_ingress.go). Decoupling the physical address from the
// per-environment identity this way means ingressHost never needs to be
// independently DNS-resolvable — it exists only inside a TLS handshake.
func assembleKubeconfig(id, apiServerExternalAddress, ingressHost string, certPEM, keyDER, caBundle []byte) (string, time.Time, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", time.Time{}, errors.New("signed certificate is not valid PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("parse signed certificate: %w", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	server := fmt.Sprintf("https://%s", apiServerExternalAddress)

	ctxName := "orbit-devenv-" + id
	cfg := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			ctxName: {Server: server, TLSServerName: ingressHost, CertificateAuthorityData: caBundle},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			ctxName: {ClientCertificateData: certPEM, ClientKeyData: keyPEM},
		},
		Contexts: map[string]*clientcmdapi.Context{
			ctxName: {Cluster: ctxName, AuthInfo: ctxName},
		},
		CurrentContext: ctxName,
	}

	out, err := clientcmd.Write(cfg)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("encode kubeconfig: %w", err)
	}
	return string(out), cert.NotAfter, nil
}

// mergeEnvironment builds the API-facing Environment view from a registry
// row and the live CR.
func mergeEnvironment(rec *Record, cr *orbitapi.OrbitDevEnvironment) *Environment {
	env := &Environment{
		ID:        rec.ID,
		Owner:     rec.Owner,
		Ref:       rec.Ref,
		CreatedAt: rec.CreatedAt,
		Phase:     orbitapi.DevEnvPhasePending,
	}
	if cr == nil {
		return env
	}
	if cr.Status.Phase != "" {
		env.Phase = cr.Status.Phase
	}
	env.Namespace = cr.Status.Namespace
	env.IngressHost = cr.Status.IngressHost
	env.AppURL = cr.Status.AppURL
	env.Awake = cr.Spec.Awake
	if cr.Status.ExpiresAt != nil {
		t := cr.Status.ExpiresAt.Time
		env.ExpiresAt = &t
	}
	return env
}

// ownerFromContext returns the verified platform token's subject — the
// only source of "who is making this request", never the request body.
func ownerFromContext(ctx context.Context) string {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		return ""
	}
	return claims.Subject
}

// ownerMatches reports whether the caller's verified principal is owner.
func ownerMatches(ctx context.Context, owner string) bool {
	caller := ownerFromContext(ctx)
	return caller != "" && caller == owner
}

// generateEnvID returns a short, DNS-1123-safe environment ID. It becomes
// both the CR name and the host namespace suffix
// ("orbit-devenv-{id}", capped at 63 characters for a namespace name), so it
// is kept well short of that budget.
func generateEnvID() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
}
