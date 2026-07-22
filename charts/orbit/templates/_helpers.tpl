{{/*
orbit chart helpers
*/}}

{{/*
Expand the name of the chart.
*/}}
{{- define "orbit.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "orbit.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Chart label.
*/}}
{{- define "orbit.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels applied to all resources in this chart.
*/}}
{{- define "orbit.labels" -}}
helm.sh/chart: {{ include "orbit.chart" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: orbit
{{- end }}

{{/*
Identity service labels — applied to the Deployment and its pod template.
*/}}
{{- define "orbit.identity.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-identity
app.kubernetes.io/component: identity
{{- end }}

{{/*
Identity service selector labels — must be stable; used by the Service selector
and the Deployment matchLabels. Do not add mutable labels here.
*/}}
{{- define "orbit.identity.selectorLabels" -}}
app: orbit-identity
{{- end }}

{{/*
Resolve the image tag for a service. Uses .Values.identity.image.tag when set,
otherwise falls back to .Chart.AppVersion. This ensures the chart always deploys
a known version even when the tag is omitted from values.
*/}}
{{- define "orbit.identity.imageTag" -}}
{{- .Values.identity.image.tag | default .Chart.AppVersion }}
{{- end }}

{{/*
Resolve the imagePullPolicy for the identity service.
Pod-level override → global default → IfNotPresent.
*/}}
{{- define "orbit.identity.imagePullPolicy" -}}
{{- .Values.identity.image.pullPolicy | default .Values.global.imagePullPolicy | default "IfNotPresent" }}
{{- end }}

{{/*
The effective trust domain — used in DestinationRule SAN URIs.
Sourced from global.trustDomain so it stays consistent with orbit-platform.
*/}}
{{- define "orbit.trustDomain" -}}
{{- required "global.trustDomain must be set — it must match the orbit-platform chart value" .Values.global.trustDomain }}
{{- end }}

{{/*
The base of every chart-pinned SPIFFE identity (DestinationRule SANs):
spiffe://<trustDomain>, plus an optional /devenv/<id> segment on Orbit
dev-environment installs, where identity is host-provided and every SVID is
path-scoped under the environment's ID. platform.spiffe.devEnvID is set by
the operator on in-vcluster installs only — never by hand, and never on a
host install. orbit.spiffePrincipalBase is the same value without the
scheme, for AuthorizationPolicy principals (Istio prepends spiffe:// itself).
*/}}
{{- define "orbit.spiffeIDBase" -}}
spiffe://{{ include "orbit.spiffePrincipalBase" . }}
{{- end }}

{{- define "orbit.spiffePrincipalBase" -}}
{{- include "orbit.trustDomain" . }}{{- with .Values.platform.spiffe.devEnvID }}/devenv/{{ . }}{{- end }}
{{- end }}

{{/*
The principal to use when checking (not pinning) a SPECIFIC workload's own
identity — e.g. an AuthorizationPolicy's source.principals/notPrincipals —
as opposed to orbit.spiffeIDBase/spiffePrincipalBase above, which describe
the trust-domain-wide base every DestinationRule SAN is pinned against.

platform.spiffe.enabled (and devEnvID) being set does NOT by itself mean
this workload's cert actually carries the custom SPIRE-style
{trustDomain}[/devenv/{id}]/system/{component} path: that path only exists
when SPIRE is the thing issuing the cert. On an in-vcluster dev-environment
install with SPIRE disabled (platform.spiffe.spireEnabled: false — no SPIRE
agent runs inside the vcluster, and no ClusterSPIFFEID selects these pods),
every workload instead gets a cert from whichever istiod actually issues
it, in Istio's own default per-ServiceAccount shape — a completely
different path the SPIRE-style string never matches (confirmed live,
2026-07-22: authorizationpolicy-policy-service.yaml's unconditional
SPIRE-style check left orbit-opa-gateway's own bundle-fetch permanently
DENYed on every SPIRE-disabled install).

The exact non-SPIRE shape is deliberately NOT reconstructed field-by-field
here (e.g. as {trustDomain}/ns/{someNamespace}/sa/{sa}): which namespace
appears depends on vcluster's own toHost sync translation and is NOT this
chart's to know — hardcoding a specific downstream repo's host-namespace
naming convention (e.g. a "vcluster-devenv-<id>" prefix) here would wire
Orbit's own chart to a naming choice made by whatever operator happens to
be running vcluster, exactly the wrong coupling direction (this chart has
callers other than the vcluster-mcp operator). Confirmed live across
multiple distinct workloads on a fresh provision (opa-gateway, identity,
policy-service): every one carried
spiffe://<trustDomain>/ns/<hostNamespace>/sa/<serviceAccount>-x-orbit-system-x-vcluster
— vcluster's own fixed, general toHost object-naming convention
({name}-x-{in-vcluster namespace}-x-vcluster), independent of whatever the
host namespace itself is called. Matching on a suffix wildcard (a leading
"star" followed by /sa/<serviceAccount>-x-<systemNamespace>-x-vcluster)
needs only what this chart already knows about itself (its own
ServiceAccount name and its own in-vcluster namespace,
orbit.systemNamespace) and sidesteps the coupling entirely. Istio's
principal matching supports exactly one wildcard, at
either end of the string (prefix match "foo*" or suffix match "*foo") —
never in the middle — which is why the namespace segment can't be pinned
down further than "*" here.

Call this with the component name used in the SPIRE-style path (e.g.
"opa-gateway") and the k8s ServiceAccount name used when SPIRE is absent
(e.g. "orbit-opa-gateway") — the two are deliberately allowed to differ,
since the SPIRE-style path is chosen by convention while the ServiceAccount
name is chosen by the chart's own templates.

Other spiffeIDBase-consuming templates in this chart (the per-service
DestinationRules) pin the SPIRE-style SAN unconditionally too, and are
likely subject to the same gap on a SPIRE-disabled install — not yet
audited/fixed here; this helper only covers the one caller
(authorizationpolicy-policy-service.yaml) confirmed broken live.
*/}}
{{- define "orbit.workloadPrincipal" -}}
{{- if .spireEnabled -}}
{{ include "orbit.spiffePrincipalBase" .root }}/system/{{ .component }}
{{- else -}}
*/sa/{{ .serviceAccount }}-x-{{ include "orbit.systemNamespace" .root }}-x-vcluster
{{- end -}}
{{- end }}

{{/*
The orbit-system namespace.
*/}}
{{- define "orbit.systemNamespace" -}}
{{- .Values.platform.namespaces.system | default "orbit-system" }}
{{- end }}

{{/*
The external frontend/BFF host (no scheme, no port), e.g. orbit.example.com.
Per the north-south exposure convention (ADR 2026-07-04), the unified backend API
host is api.<frontendHost>. Set per environment (deploy/local for dev, a prod
overlay for prod) — never hardcode a domain in the chart.
*/}}
{{- define "orbit.frontendHost" -}}
{{- required "frontendHost must be set — the external host of the frontend/BFF, e.g. orbit.example.com" .Values.frontendHost }}
{{- end }}

{{/*
The unified backend/data-plane API host: api.<frontendHost>.
*/}}
{{- define "orbit.apiHost" -}}
api.{{ include "orbit.frontendHost" . }}
{{- end }}

{{/*
Inference gateway service labels — applied to the Deployment and its pod template.
*/}}
{{- define "orbit.inferenceGateway.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-inference-gateway
app.kubernetes.io/component: inference-gateway
{{- end }}

{{/*
Inference gateway selector labels — stable; used by Service selector and Deployment matchLabels.
*/}}
{{- define "orbit.inferenceGateway.selectorLabels" -}}
app: orbit-inference-gateway
{{- end }}

{{/*
Resolve the image tag for the inference gateway.
*/}}
{{- define "orbit.inferenceGateway.imageTag" -}}
{{- .Values.inferenceGateway.image.tag | default .Chart.AppVersion }}
{{- end }}

{{/*
Resolve the imagePullPolicy for the inference gateway.
Pod-level override → global default → IfNotPresent.
*/}}
{{- define "orbit.inferenceGateway.imagePullPolicy" -}}
{{- .Values.inferenceGateway.image.pullPolicy | default .Values.global.imagePullPolicy | default "IfNotPresent" }}
{{- end }}

{{/*
Orbit MCP service labels — applied to the Deployment and its pod template.
*/}}
{{- define "orbit.orbitMcp.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-mcp
app.kubernetes.io/component: orbit-mcp
{{- end }}

{{/*
Orbit MCP selector labels — stable; used by Service selector and Deployment matchLabels.
*/}}
{{- define "orbit.orbitMcp.selectorLabels" -}}
app: orbit-mcp
{{- end }}

{{/*
Resolve the image tag for orbit-mcp.
*/}}
{{- define "orbit.orbitMcp.imageTag" -}}
{{- .Values.orbitMcp.image.tag | default .Chart.AppVersion }}
{{- end }}

{{/*
Resolve the imagePullPolicy for orbit-mcp.
Pod-level override → global default → IfNotPresent.
*/}}
{{- define "orbit.orbitMcp.imagePullPolicy" -}}
{{- .Values.orbitMcp.image.pullPolicy | default .Values.global.imagePullPolicy | default "IfNotPresent" }}
{{- end }}

{{/*
Tool-gateway service labels — applied to the Deployment and its pod template.
*/}}
{{- define "orbit.toolGateway.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-tool-gateway
app.kubernetes.io/component: tool-gateway
{{- end }}

{{/*
Tool-gateway selector labels — stable; used by Service selector and Deployment matchLabels.
*/}}
{{- define "orbit.toolGateway.selectorLabels" -}}
app: orbit-tool-gateway
{{- end }}

{{/*
Resolve the image tag for the tool-gateway.
*/}}
{{- define "orbit.toolGateway.imageTag" -}}
{{- .Values.toolGateway.image.tag | default .Chart.AppVersion }}
{{- end }}

{{/*
Resolve the imagePullPolicy for the tool-gateway.
Pod-level override → global default → IfNotPresent.
*/}}
{{- define "orbit.toolGateway.imagePullPolicy" -}}
{{- .Values.toolGateway.image.pullPolicy | default .Values.global.imagePullPolicy | default "IfNotPresent" }}
{{- end }}

{{/*
Frontend service labels — applied to the Deployment and its pod template.
*/}}
{{- define "orbit.frontend.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-frontend
app.kubernetes.io/component: frontend
{{- end }}

{{/*
Frontend service selector labels — must be stable; used by the Service selector
and the Deployment matchLabels. Do not add mutable labels here.
*/}}
{{- define "orbit.frontend.selectorLabels" -}}
app: orbit-frontend
{{- end }}

{{/*
Resolve the image tag for the frontend. Uses .Values.frontend.image.tag when set,
otherwise falls back to .Chart.AppVersion.
*/}}
{{- define "orbit.frontend.imageTag" -}}
{{- .Values.frontend.image.tag | default .Chart.AppVersion }}
{{- end }}

{{/*
Resolve the imagePullPolicy for the frontend service.
Pod-level override → global default → IfNotPresent.
*/}}
{{- define "orbit.frontend.imagePullPolicy" -}}
{{- .Values.frontend.image.pullPolicy | default .Values.global.imagePullPolicy | default "IfNotPresent" }}
{{- end }}

{{/*
Policy Service labels.
*/}}
{{- define "orbit.policyService.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-policy-service
app.kubernetes.io/component: policy-service
{{- end }}

{{- define "orbit.policyService.selectorLabels" -}}
app: orbit-policy-service
{{- end }}

{{- define "orbit.policyService.imageTag" -}}
{{- .Values.policyService.image.tag | default .Chart.AppVersion }}
{{- end }}

{{- define "orbit.policyService.imagePullPolicy" -}}
{{- .Values.policyService.image.pullPolicy | default .Values.global.imagePullPolicy | default "IfNotPresent" }}
{{- end }}

{{/*
Policy Gateway labels.
*/}}
{{- define "orbit.policyGateway.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-policy-gateway
app.kubernetes.io/component: policy-gateway
{{- end }}

{{- define "orbit.policyGateway.selectorLabels" -}}
app: orbit-policy-gateway
{{- end }}

{{- define "orbit.policyGateway.imageTag" -}}
{{- .Values.policyGateway.image.tag | default .Chart.AppVersion }}
{{- end }}

{{- define "orbit.policyGateway.imagePullPolicy" -}}
{{- .Values.policyGateway.image.pullPolicy | default .Values.global.imagePullPolicy | default "IfNotPresent" }}
{{- end }}

{{/*
Egress Gateway labels.
Per-tenant credential broker that hijacks CONNECT requests from sandbox pods,
terminates upstream TLS with a per-tenant CA leaf, mints just-in-time
upstream credentials, and forwards to the real upstream. See
docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md.
*/}}
{{- define "orbit.egressGateway.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-egress-gateway
app.kubernetes.io/component: egress-gateway
{{- end }}

{{- define "orbit.egressGateway.selectorLabels" -}}
app: orbit-egress-gateway
{{- end }}

{{- define "orbit.egressGateway.imageTag" -}}
{{- .Values.egressGateway.image.tag | default .Chart.AppVersion }}
{{- end }}

{{- define "orbit.egressGateway.imagePullPolicy" -}}
{{- .Values.egressGateway.image.pullPolicy | default .Values.global.imagePullPolicy | default "IfNotPresent" }}
{{- end }}

{{/*
OPA Gateway labels.
Standalone OPA pod that enforces authz_gateway.rego on the Istio IngressGateway
via ext_authz (ADR-0044).
*/}}
{{- define "orbit.opaGateway.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-opa-gateway
app.kubernetes.io/component: opa-gateway
{{- end }}

{{- define "orbit.opaGateway.selectorLabels" -}}
app: orbit-opa-gateway
{{- end }}

{{/*
Agents service labels — orbit-agents manages the agent/run lifecycle and
provisions StatefulSets, sandboxes, and services in tenant namespaces.
*/}}
{{- define "orbit.agents.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-agents
app.kubernetes.io/component: agents
{{- end }}

{{- define "orbit.agents.selectorLabels" -}}
app: orbit-agents
{{- end }}

{{- define "orbit.agents.imageTag" -}}
{{- .Values.agents.image.tag | default .Chart.AppVersion }}
{{- end }}

{{- define "orbit.agents.imagePullPolicy" -}}
{{- .Values.agents.image.pullPolicy | default .Values.global.imagePullPolicy | default "IfNotPresent" }}
{{- end }}
