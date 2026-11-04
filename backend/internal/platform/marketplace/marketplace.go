// Package marketplace is the contract between the operator and skills-service
// for SkillMarketplaces. The operator renders every served Marketplace into
// the ConfigMap ConfigMapName and copies each GitHub App private key into the
// Secret SecretName, both in the system namespace. skills-service mounts both
// and reads the file through File.
//
// See docs/adr/2026-08-17-agent-skills.md.
package marketplace

const (
	// ConfigMapName is the system-namespace ConfigMap holding FileName.
	ConfigMapName = "kora-skill-marketplaces"
	// FileName is the ConfigMap data key, and the file skills-service reads.
	FileName = "marketplaces.json"
	// SecretName is the system-namespace Secret holding every private key,
	// keyed by SecretKey.
	SecretName = "kora-skill-marketplace-secrets"
)

// File is the content of FileName. Tenants are in ID order.
type File struct {
	Tenants []Tenant `json:"tenants"`
}

// Tenant is one tenant's Marketplaces, oldest first. ID is the KoraTenant name.
type Tenant struct {
	ID           string        `json:"id"`
	Marketplaces []Marketplace `json:"marketplaces"`
}

// Marketplace is one SkillMarketplace as skills-service needs it.
type Marketplace struct {
	// Slug is the SkillMarketplace metadata.name.
	Slug  string `json:"slug"`
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	// Ref is a branch or commit SHA. Empty means the default branch.
	Ref            string `json:"ref,omitempty"`
	AppID          string `json:"appID"`
	InstallationID int64  `json:"installationID"`
	// PrivateKeySecretKey is the data key of the App private key in SecretName.
	PrivateKeySecretKey string `json:"privateKeySecretKey"`
}

// SecretKey is the SecretName data key for a Marketplace's private key.
// Namespaces hold no dots, so the first dot separates the two parts.
func SecretKey(namespace, name string) string {
	return namespace + "." + name
}
