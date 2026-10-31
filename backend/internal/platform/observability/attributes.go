package observability

import "go.opentelemetry.io/otel/attribute"

// Span attribute keys outside the Go semconv package. Every span uses these or
// a semconv constant, so one identifier has one key across services
// (docs/adr/2026-03-21-opentelemetry-observability-discipline.md, Amendment 1).
const (
	TenantIDKey                attribute.Key = "kora.tenant.id"
	RunIDKey                   attribute.Key = "kora.run.id"
	RunStateKey                attribute.Key = "kora.run.state"
	RunStateFromKey            attribute.Key = "kora.run.state_from"
	RunStateToKey              attribute.Key = "kora.run.state_to"
	PrincipalIDKey             attribute.Key = "kora.principal.id"
	PrincipalTypeKey           attribute.Key = "kora.principal.type"
	PrincipalIdentitySourceKey attribute.Key = "kora.principal.identity_source"
	AgentIDKey                 attribute.Key = "kora.agent.id"
	AgentIDsKey                attribute.Key = "kora.agent.ids"
	AgentIDCountKey            attribute.Key = "kora.agent.id_count"
	AgentSlugKey               attribute.Key = "kora.agent.slug"
	AgentUpdateKey             attribute.Key = "kora.agent.update"
	TriggerIDKey               attribute.Key = "kora.trigger.id"
	TriggerDeliveryIDKey       attribute.Key = "kora.trigger.delivery.id"
	EnvironmentIDKey           attribute.Key = "kora.environment.id"
	EnvironmentNameKey         attribute.Key = "kora.environment.name"
	EnvironmentReplaceKey      attribute.Key = "kora.environment.replace"
	DraftIDKey                 attribute.Key = "kora.draft.id"
	SandboxEndpointKey         attribute.Key = "kora.sandbox.endpoint"
	PageLimitKey               attribute.Key = "kora.page.limit"

	PATIDKey                attribute.Key = "kora.pat.id"
	PATNameKey              attribute.Key = "kora.pat.name"
	PATTTLDaysKey           attribute.Key = "kora.pat.ttl_days"
	SessionIDKey            attribute.Key = "kora.session.id"
	TokenJTIKey             attribute.Key = "kora.token.jti"
	TokenTTLSecondsKey      attribute.Key = "kora.token.ttl_seconds"
	TokenTTLMinutesKey      attribute.Key = "kora.token.ttl_minutes"
	TokenExpiresInKey       attribute.Key = "kora.token.expires_in"
	TokenExpiryKey          attribute.Key = "kora.token.expiry"
	TokenRefreshAttemptsKey attribute.Key = "kora.token.refresh_attempts"
	RefreshTokenIDKey       attribute.Key = "kora.refresh_token.id"
	RefreshTokenTTLDaysKey  attribute.Key = "kora.refresh_token.ttl_days"
	OAuthGrantTypeKey       attribute.Key = "kora.oauth.grant_type"
	OAuthTokenTypeKey       attribute.Key = "kora.oauth.token_type"
	OAuthErrorKey           attribute.Key = "kora.oauth.error"
	DelegationResultKey     attribute.Key = "kora.delegation.result"

	ErrorCodeKey            attribute.Key = "kora.error.code"
	ErrorMessageKey         attribute.Key = "kora.error.message"
	ErrorUpstreamBodyKey    attribute.Key = "kora.error.upstream_body"
	ErrorUpstreamMessageKey attribute.Key = "kora.error.upstream_message"

	AnalyticsScopeKey          attribute.Key = "kora.analytics.scope"
	AnalyticsDaysKey           attribute.Key = "kora.analytics.days"
	AnalyticsGranularityKey    attribute.Key = "kora.analytics.granularity"
	AnalyticsPrincipalsDaysKey attribute.Key = "kora.analytics.principals.days"
	AnalyticsPrincipalsPageKey attribute.Key = "kora.analytics.principals.page"
	InferenceRequestStreamKey  attribute.Key = "kora.inference.request.stream"

	MCPServerKey   attribute.Key = "kora.mcp.server"
	MCPDecisionKey attribute.Key = "kora.mcp.decision"

	ConnectionNameKey          attribute.Key = "kora.connection.name"
	ConnectionAdapterKey       attribute.Key = "kora.connection.adapter"
	ConnectionRegisteredKey    attribute.Key = "kora.connection.registered"
	EgressActionKey            attribute.Key = "kora.egress.action"
	EgressQualifierKey         attribute.Key = "kora.egress.qualifier"
	EgressAllowedKey           attribute.Key = "kora.egress.allowed"
	EgressAlsoAuthorizedKey    attribute.Key = "kora.egress.also_authorized"
	EgressNodeIDCountKey       attribute.Key = "kora.egress.node_id_count"
	EgressRepositoryIDCountKey attribute.Key = "kora.egress.repository_id_count"
	RelayBytesSentKey          attribute.Key = "kora.relay.bytes_sent"
	RelayBytesReceivedKey      attribute.Key = "kora.relay.bytes_received"
	DBRowsAffectedKey          attribute.Key = "kora.db.rows_affected"
)

// Semantic convention keys with development status, which the Go semconv
// package does not export.
const (
	GenAIOperationNameKey attribute.Key = "gen_ai.operation.name"
	GenAIRequestModelKey  attribute.Key = "gen_ai.request.model"
	GenAIToolNameKey      attribute.Key = "gen_ai.tool.name"
	MCPMethodNameKey      attribute.Key = "mcp.method.name"
)
