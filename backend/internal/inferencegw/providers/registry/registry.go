// Package registry builds the gateway's provider connectors from
// InferenceProvider entries and from the deprecated provider env vars, and
// swaps them into the routing engine whenever the mounted entries change.
//
// See docs/adr/2026-10-01-crd-inference-providers.md.
package registry

import (
	"context"
	"fmt"
	"slices"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/anthropicapi"
	"github.com/clear-route/orbit/internal/inferencegw/providers/azure"
	"github.com/clear-route/orbit/internal/inferencegw/providers/bedrock"
	"github.com/clear-route/orbit/internal/inferencegw/providers/openaicompat"
	"github.com/clear-route/orbit/internal/inferencegw/providers/vertexai"
	ip "github.com/clear-route/orbit/internal/platform/inferenceproviders"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// Built is a connector and its admin description. Connector is nil when the
// build failed; Info.BuildError says why.
type Built struct {
	Connector inferencegw.ProviderConnector
	Info      inferencegw.ProviderInfo
}

// FromResource builds the connector for one InferenceProvider entry.
func FromResource(p ip.Provider, logger logging.Logger) Built {
	name := inferencegw.ProviderName(p.Name)
	info := inferencegw.ProviderInfo{
		Name:        name,
		Type:        string(p.Type),
		DisplayName: p.DisplayName,
		Source:      inferencegw.ProviderSourceResource,
		Resource:    p.Resource,
		Models:      p.Models,
	}
	if err := p.Validate(); err != nil {
		info.BuildError = err.Error()
		return Built{Info: info}
	}
	conn, err := build(p, name, &info, logger)
	if err != nil {
		info.BuildError = err.Error()
		return Built{Info: info}
	}
	return Built{Connector: withModelList(conn, p.Type, p.Models), Info: info}
}

// build constructs the connector for p's type and fills the non-secret parts
// of info.
func build(p ip.Provider, name inferencegw.ProviderName, info *inferencegw.ProviderInfo, logger logging.Logger) (inferencegw.ProviderConnector, error) {
	switch p.Type {
	case ip.TypeBedrock:
		b := p.Bedrock
		info.Location = b.Region
		info.AssumeRoleARN = b.AssumeRoleARN
		info.CredentialMode = credentialMode(b.AccessKeyIDKey != "", true)
		cfg := bedrock.Config{Region: b.Region, AssumeRoleARN: b.AssumeRoleARN, ExternalID: b.ExternalID}
		var err error
		if cfg.AccessKeyID, err = p.Secret(b.AccessKeyIDKey); err != nil {
			return nil, err
		}
		if cfg.SecretAccessKey, err = p.Secret(b.SecretAccessKeyKey); err != nil {
			return nil, err
		}
		if cfg.SessionToken, err = p.Secret(b.SessionTokenKey); err != nil {
			return nil, err
		}
		return bedrock.New(&cfg, name, logger)

	case ip.TypeVertexAI:
		v := p.VertexAI
		info.Location = v.ProjectID + "/" + v.Location
		info.CredentialMode = credentialMode(v.CredentialsKey != "", true)
		key, err := p.Secret(v.CredentialsKey)
		if err != nil {
			return nil, err
		}
		anthropicLocation := v.AnthropicLocation
		if anthropicLocation == "" {
			anthropicLocation = v.Location
		}
		return vertexai.New(&vertexai.Config{
			ProjectID:         v.ProjectID,
			Location:          v.Location,
			AnthropicLocation: anthropicLocation,
			CredentialsJSON:   []byte(key),
		}, name, logger)

	case ip.TypeAzure:
		a := p.Azure
		info.Location = a.Endpoint
		info.CredentialMode = inferencegw.CredentialModeSecret
		key, err := p.Secret(a.APIKeyKey)
		if err != nil {
			return nil, err
		}
		anthropicEndpoint := a.AnthropicEndpoint
		if anthropicEndpoint == "" {
			anthropicEndpoint = a.Endpoint
		}
		return azure.New(&azure.Config{
			Endpoint:          a.Endpoint,
			APIKey:            key,
			APIVersion:        a.APIVersion,
			AnthropicEndpoint: anthropicEndpoint,
		}, name, logger)

	case ip.TypeOpenAI:
		o := p.OpenAI
		info.Location = o.BaseURL
		info.CredentialMode = credentialMode(o.APIKeyKey != "", false)
		key, err := p.Secret(o.APIKeyKey)
		if err != nil {
			return nil, err
		}
		return openaicompat.New(openaicompat.Config{BaseURL: o.BaseURL, APIKey: key, CACert: o.CACert}, name, logger)

	case ip.TypeAnthropic:
		a := p.Anthropic
		info.Location = a.BaseURL
		info.CredentialMode = inferencegw.CredentialModeSecret
		key, err := p.Secret(a.APIKeyKey)
		if err != nil {
			return nil, err
		}
		return anthropicapi.New(anthropicapi.Config{BaseURL: a.BaseURL, APIKey: key, CACert: a.CACert}, name, logger)
	}
	return nil, fmt.Errorf("unknown provider type %q", p.Type)
}

// credentialMode names how a provider authenticates. ambient reports whether
// the type falls back to workload identity without a Secret.
func credentialMode(hasSecret, ambient bool) string {
	switch {
	case hasSecret:
		return inferencegw.CredentialModeSecret
	case ambient:
		return inferencegw.CredentialModeWorkloadIdentity
	default:
		return inferencegw.CredentialModeNone
	}
}

// FromEnv builds the providers configured by the deprecated BEDROCK_*,
// VERTEX_* and AZURE_* env vars. A misconfigured env var is an error, as it
// was before resources existed.
func FromEnv(ctx context.Context, logger logging.Logger) ([]Built, error) {
	var out []Built
	bedrockCfgs, err := bedrock.LoadMultiConfig()
	if err != nil {
		return nil, err
	}
	for _, nc := range bedrockCfgs {
		name := inferencegw.ProviderName("bedrock:" + nc.Alias)
		conn, buildErr := bedrock.New(&nc.Config, name, logger)
		if buildErr != nil {
			return nil, fmt.Errorf("bedrock account %q: %w", nc.Alias, buildErr)
		}
		out = append(out, envBuilt(conn, string(ip.TypeBedrock), nc.Config.Region,
			credentialMode(nc.Config.AccessKeyID != "", true)))
	}

	vcfg, err := vertexai.LoadConfig()
	if err != nil {
		return nil, err
	}
	if vcfg != nil {
		conn, buildErr := vertexai.New(vcfg, inferencegw.ProviderVertexAI, logger)
		if buildErr != nil {
			return nil, buildErr
		}
		out = append(out, envBuilt(conn, string(ip.TypeVertexAI), vcfg.ProjectID+"/"+vcfg.Location,
			inferencegw.CredentialModeWorkloadIdentity))
	}

	acfg, err := azure.LoadConfig()
	if err != nil {
		return nil, err
	}
	if acfg != nil {
		conn, buildErr := azure.New(acfg, inferencegw.ProviderAzure, logger)
		if buildErr != nil {
			return nil, buildErr
		}
		out = append(out, envBuilt(conn, string(ip.TypeAzure), acfg.Endpoint, inferencegw.CredentialModeSecret))
	}

	for _, b := range out {
		logger.Warn(ctx, "provider configured by env vars; env provider configuration is deprecated, register an InferenceProvider instead",
			logging.Field{Key: "provider", Value: string(b.Info.Name)})
	}
	return out, nil
}

func envBuilt(conn inferencegw.ProviderConnector, typ, location, mode string) Built {
	return Built{Connector: conn, Info: inferencegw.ProviderInfo{
		Name:           conn.Name(),
		Type:           typ,
		Source:         inferencegw.ProviderSourceEnvironment,
		Location:       location,
		CredentialMode: mode,
	}}
}

// Merge combines env and resource providers. A resource replaces an env
// provider of the same name. Failed builds keep their info and get no
// connector.
func Merge(env, resources []Built) (map[inferencegw.ProviderName]inferencegw.ProviderConnector, []inferencegw.ProviderInfo) {
	connectors := map[inferencegw.ProviderName]inferencegw.ProviderConnector{}
	var infos []inferencegw.ProviderInfo
	fromResource := map[inferencegw.ProviderName]bool{}
	for _, b := range resources {
		fromResource[b.Info.Name] = true
	}
	for _, b := range slices.Concat(env, resources) {
		if b.Info.Source == inferencegw.ProviderSourceEnvironment && fromResource[b.Info.Name] {
			continue
		}
		infos = append(infos, b.Info)
		if b.Connector != nil {
			connectors[b.Info.Name] = b.Connector
		}
	}
	return connectors, infos
}
