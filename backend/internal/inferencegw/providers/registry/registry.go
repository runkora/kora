// Package registry builds the gateway's provider connectors from the
// InferenceProvider resources it watches.
//
// See docs/adr/2026-10-01-crd-inference-providers.md.
package registry

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/azure"
	"github.com/clear-route/orbit/internal/inferencegw/providers/bedrock"
	"github.com/clear-route/orbit/internal/inferencegw/providers/compatible"
	"github.com/clear-route/orbit/internal/inferencegw/providers/vertexai"
	ip "github.com/clear-route/orbit/internal/platform/inferenceproviders"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// Reasons on the status condition of a resource.
const (
	ReasonLoaded            = "Loaded"
	ReasonInvalidSpec       = "InvalidSpec"
	ReasonSecretNotFound    = "SecretNotFound"
	ReasonSecretKeyNotFound = "SecretKeyNotFound"
	ReasonBuildFailed       = "BuildFailed"
)

// Bedrock credential Secret keys.
const (
	keyAccessKeyID     = "accessKeyID"
	keySecretAccessKey = "secretAccessKey"
	keySessionToken    = "sessionToken"
)

// BuildError is why a resource produced no connector.
type BuildError struct {
	Reason  string
	Message string
}

func (e *BuildError) Error() string { return e.Message }

func buildErr(reason, format string, args ...any) error {
	return &BuildError{Reason: reason, Message: fmt.Sprintf(format, args...)}
}

// SecretReader returns one key of a Secret in the resource's namespace. It
// returns a *BuildError when the Secret or the key is missing.
type SecretReader func(ctx context.Context, name, key string) (string, error)

// Build constructs the connector for one resource and describes it for the
// admin view. The description never carries a credential.
func Build(ctx context.Context, p *ip.InferenceProvider, secret SecretReader, logger logging.Logger) (inferencegw.ProviderConnector, inferencegw.ProviderInfo, error) {
	info := inferencegw.ProviderInfo{
		Name:        inferencegw.ProviderName(p.ProviderName()),
		Type:        string(p.Spec.Type),
		DisplayName: p.Spec.DisplayName,
		Resource:    p.Name,
		Models:      p.Spec.Models,
	}
	conn, err := build(ctx, p.Spec, &info, secret, logger)
	if err != nil {
		var be *BuildError
		if !errors.As(err, &be) {
			err = &BuildError{Reason: ReasonBuildFailed, Message: err.Error()}
		}
		info.BuildError = err.Error()
		return nil, info, err
	}
	return withModelList(conn, p.Spec.Models), info, nil
}

func build(ctx context.Context, s ip.Spec, info *inferencegw.ProviderInfo, secret SecretReader, logger logging.Logger) (inferencegw.ProviderConnector, error) {
	switch {
	case s.Type == ip.TypeBedrock && s.Bedrock != nil:
		return buildBedrock(ctx, s.Bedrock, info, secret, logger)
	case s.Type == ip.TypeVertexAI && s.VertexAI != nil:
		return buildVertexAI(ctx, s.VertexAI, info, secret, logger)
	case s.Type == ip.TypeAzure && s.Azure != nil:
		return buildAzure(ctx, s.Azure, info, secret, logger)
	case s.Type == ip.TypeCompatible && s.Compatible != nil:
		return buildCompatible(ctx, s.Compatible, info, secret, logger)
	}
	return nil, buildErr(ReasonInvalidSpec, "type %q needs its own config block", s.Type)
}

func buildBedrock(ctx context.Context, b *ip.Bedrock, info *inferencegw.ProviderInfo, secret SecretReader, logger logging.Logger) (inferencegw.ProviderConnector, error) {
	info.Location = b.Region
	info.AssumeRoleARN = b.AssumeRoleARN
	info.CredentialMode = inferencegw.CredentialModeWorkloadIdentity
	cfg := bedrock.Config{Region: b.Region, AssumeRoleARN: b.AssumeRoleARN, ExternalID: b.ExternalID, RoleSessionName: b.RoleSessionName}
	if b.Credentials != nil {
		info.CredentialMode = inferencegw.CredentialModeSecret
		if err := readBedrockKeys(ctx, secret, b.Credentials.SecretRef.Name, &cfg); err != nil {
			return nil, err
		}
	}
	return bedrock.New(&cfg, info.Name, logger)
}

func buildVertexAI(ctx context.Context, v *ip.VertexAI, info *inferencegw.ProviderInfo, secret SecretReader, logger logging.Logger) (inferencegw.ProviderConnector, error) {
	info.Location = v.ProjectID + "/" + v.Location
	info.CredentialMode = inferencegw.CredentialModeWorkloadIdentity
	cfg := &vertexai.Config{ProjectID: v.ProjectID, Location: v.Location, AnthropicLocation: v.AnthropicLocation}
	if cfg.AnthropicLocation == "" {
		cfg.AnthropicLocation = v.Location
	}
	if ref := v.CredentialsSecretRef; ref != nil {
		info.CredentialMode = inferencegw.CredentialModeSecret
		key, err := secret(ctx, ref.Name, ref.Key)
		if err != nil {
			return nil, err
		}
		cfg.CredentialsJSON = []byte(key)
	}
	return vertexai.New(cfg, info.Name, logger)
}

func buildAzure(ctx context.Context, a *ip.Azure, info *inferencegw.ProviderInfo, secret SecretReader, logger logging.Logger) (inferencegw.ProviderConnector, error) {
	info.Location = a.Endpoint
	info.CredentialMode = inferencegw.CredentialModeSecret
	key, err := secret(ctx, a.APIKeySecretRef.Name, a.APIKeySecretRef.Key)
	if err != nil {
		return nil, err
	}
	cfg := &azure.Config{Endpoint: a.Endpoint, APIKey: key, APIVersion: a.APIVersion, AnthropicEndpoint: a.AnthropicEndpoint}
	if cfg.AnthropicEndpoint == "" {
		cfg.AnthropicEndpoint = a.Endpoint
	}
	return azure.New(cfg, info.Name, logger)
}

func buildCompatible(ctx context.Context, c *ip.Compatible, info *inferencegw.ProviderInfo, secret SecretReader, logger logging.Logger) (inferencegw.ProviderConnector, error) {
	info.Location = c.BaseURL
	info.CredentialMode = inferencegw.CredentialModeNone
	cfg := compatible.Config{BaseURL: c.BaseURL, CACert: c.CACert, APIs: c.APIs}
	if ref := c.APIKeySecretRef; ref != nil {
		info.CredentialMode = inferencegw.CredentialModeSecret
		key, err := secret(ctx, ref.Name, ref.Key)
		if err != nil {
			return nil, err
		}
		cfg.APIKey = key
	}
	conn, err := compatible.New(cfg, info.Name, logger)
	if err != nil {
		return nil, buildErr(ReasonInvalidSpec, "%s", err.Error())
	}
	return conn, nil
}

// readBedrockKeys reads a static key pair and an optional session token.
func readBedrockKeys(ctx context.Context, secret SecretReader, ref string, cfg *bedrock.Config) error {
	var err error
	if cfg.AccessKeyID, err = secret(ctx, ref, keyAccessKeyID); err != nil {
		return err
	}
	if cfg.SecretAccessKey, err = secret(ctx, ref, keySecretAccessKey); err != nil {
		return err
	}
	cfg.SessionToken, err = secret(ctx, ref, keySessionToken)
	var be *BuildError
	if errors.As(err, &be) && be.Reason == ReasonSecretKeyNotFound {
		return nil
	}
	return err
}

// describer builds a catalog entry for a model known only by its id.
type describer interface {
	Describe(id string) inferencegw.ProviderModel
}

// modelListConnector offers only the models a resource lists in spec.models.
// When discovery finds none of them, the list itself is the catalog; some
// compatible servers have no model list.
type modelListConnector struct {
	inferencegw.ProviderConnector
	models []string
}

func withModelList(conn inferencegw.ProviderConnector, models []string) inferencegw.ProviderConnector {
	if len(models) == 0 {
		return conn
	}
	return &modelListConnector{ProviderConnector: conn, models: models}
}

// ListModels filters discovery to the listed models.
func (c *modelListConnector) ListModels(ctx context.Context) ([]inferencegw.ProviderModel, error) {
	discovered, _ := c.ProviderConnector.ListModels(ctx)
	var kept []inferencegw.ProviderModel
	for _, m := range discovered {
		if slices.Contains(c.models, m.ModelID) || slices.ContainsFunc(m.Aliases, func(a string) bool {
			return slices.Contains(c.models, a)
		}) {
			kept = append(kept, m)
		}
	}
	if len(kept) > 0 {
		return kept, nil
	}
	out := make([]inferencegw.ProviderModel, 0, len(c.models))
	for _, id := range c.models {
		if d, ok := c.ProviderConnector.(describer); ok {
			out = append(out, d.Describe(id))
			continue
		}
		out = append(out, inferencegw.ProviderModel{
			Provider:     c.Name(),
			ModelID:      id,
			Family:       inferencegw.InferFamily(id),
			DisplayName:  id,
			Dialects:     []inferencegw.APIDialect{inferencegw.DialectOpenAI},
			Capabilities: []inferencegw.ModelCapability{inferencegw.CapabilityChat},
		})
	}
	return out, nil
}
