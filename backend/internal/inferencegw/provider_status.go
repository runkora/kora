package inferencegw

import (
	"sort"
	"time"
)

// Provider sources.
const (
	ProviderSourceResource    = "resource"
	ProviderSourceEnvironment = "environment"
)

// Provider credential modes shown to admins. None of them carries a value.
const (
	CredentialModeSecret           = "secret"
	CredentialModeWorkloadIdentity = "workload identity"
	CredentialModeNone             = "none"
)

// Provider statuses.
const (
	ProviderStatusReady = "ready"
	ProviderStatusError = "error"
)

// ProviderInfo describes one configured provider. BuildError is set when its
// connector could not be built; the provider then serves no traffic.
type ProviderInfo struct {
	Name           ProviderName `json:"name"`
	Type           string       `json:"type"`
	DisplayName    string       `json:"display_name,omitempty"`
	Source         string       `json:"source"`
	Resource       string       `json:"resource,omitempty"`
	Location       string       `json:"location,omitempty"`
	CredentialMode string       `json:"credential_mode"`
	AssumeRoleARN  string       `json:"assume_role_arn,omitempty"`
	Models         []string     `json:"models,omitempty"`
	BuildError     string       `json:"-"`
}

// discoveryResult is the outcome of one provider's last ListModels call.
type discoveryResult struct {
	at     time.Time
	models int
	err    string
}

// ProviderStatus is a provider with its runtime health.
type ProviderStatus struct {
	ProviderInfo
	Status           string     `json:"status"`
	Error            string     `json:"error,omitempty"`
	ModelCount       int        `json:"model_count"`
	LastDiscoveredAt *time.Time `json:"last_discovered_at,omitempty"`
}

// ProviderStatuses returns every configured provider with its health, sorted by
// name. A build error or a failed discovery marks it as an error.
func (r *RoutingEngine) ProviderStatuses() []ProviderStatus {
	var infos []ProviderInfo
	if p := r.providers.Load(); p != nil {
		infos = *p
	}
	var discovered map[ProviderName]discoveryResult
	if d := r.discovery.Load(); d != nil {
		discovered = *d
	}
	out := make([]ProviderStatus, 0, len(infos))
	for _, info := range infos {
		st := ProviderStatus{ProviderInfo: info, Status: ProviderStatusReady}
		if d, ok := discovered[info.Name]; ok {
			at := d.at
			st.LastDiscoveredAt = &at
			st.ModelCount = d.models
			st.Error = d.err
		}
		if info.BuildError != "" {
			st.Error = info.BuildError
		}
		if st.Error != "" {
			st.Status = ProviderStatusError
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
