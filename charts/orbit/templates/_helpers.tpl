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
See docs/adr/2026-08-04-devenv-mode-platform-support.md.
*/}}
{{- define "orbit.spiffeIDBase" -}}
spiffe://{{ include "orbit.spiffePrincipalBase" . }}
{{- end }}

{{- define "orbit.spiffePrincipalBase" -}}
{{- include "orbit.trustDomain" . }}{{- with .Values.platform.spiffe.devEnvID }}/devenv/{{ . }}{{- end }}
{{- end }}

{{/*
The principal(s) to use when checking (not pinning) a SPECIFIC workload's
own identity — e.g. an AuthorizationPolicy's source.principals/
notPrincipals — as opposed to orbit.spiffeIDBase/spiffePrincipalBase above,
which describe the trust-domain-wide base every DestinationRule SAN is
pinned against. Renders a YAML list (one or more "- ..." lines); the caller
splices it directly into a notPrincipals/principals block.

Why more than one candidate: platform.spiffe.enabled (and devEnvID) being
set does NOT by itself mean this workload's cert reliably carries ONE
predictable shape. On an in-vcluster dev-environment install with SPIRE
disabled on the OrbitPlatform CR itself (platform.spiffe.spireEnabled:
false — Orbit's OWN in-vcluster chart doesn't run SPIRE or render its own
ClusterSPIFFEID templates), the environment provisioner's OWN, entirely
separate host-side identity mechanism (host ClusterSPIFFEIDs matching any
pod with a stable spiffe.io/component label, attested by the HOST's real
SPIRE agent since every synced pod is a real host pod) can STILL grant a
genuine SPIRE-issued SVID in the usual
{trustDomain}/devenv/{id}/system/{component} shape — spireEnabled on this
install and "does this pod actually get a SPIRE cert" are orthogonal
questions once a devenv provisioner with its own host-SPIRE mechanism is in
the picture. Confirmed live (see docs/adr/2026-08-04-devenv-mode-platform-support.md,
Decision 4), across repeated fresh provisions of the identical workload, its
actual cert SAN was NOT stable — it was observed as the SPIRE-style devenv
path on one provision and as istiod's own default per-ServiceAccount
fallback path on another, with no code change in between. This is a genuine
race between the host SPIRE agent's CSI-driver-delivered SVID and istiod's
own cert issuance at pod startup, not a deterministic either/or — so BOTH
plausible shapes are exempted rather than picking one and leaving the other
to intermittently 403.

The istiod-fallback shape is deliberately NOT reconstructed field-by-field
(e.g. as {trustDomain}/ns/{someNamespace}/sa/{sa}): which namespace appears
depends on the provisioner's own sync/translation layer and is NOT this
chart's to know — hardcoding a specific downstream repo's host-namespace
naming convention here would wire Orbit's own chart to a naming choice made
by whatever operator happens to be running the environment, the wrong
coupling direction for a chart with callers beyond one provisioner.
Matching on a suffix wildcard (a leading "*" followed by
/sa/<serviceAccount>-x-<systemNamespace>-x-vcluster) needs only what this
chart already knows about itself (its own ServiceAccount name and its own
in-vcluster namespace, orbit.systemNamespace) and sidesteps the coupling
entirely. Istio's principal matching supports exactly one wildcard, at
either end of the string (prefix match "foo*" or suffix match "*foo") —
never in the middle — which is why the namespace segment can't be pinned
down further than "*" here.

Call this with the component name used in the SPIRE-style path (e.g.
"opa-gateway") and the k8s ServiceAccount name used on the istiod-fallback
path (e.g. "orbit-opa-gateway") — the two are deliberately allowed to
differ, since the SPIRE-style path is chosen by convention while the
ServiceAccount name is chosen by the chart's own templates.
*/}}
{{- define "orbit.workloadPrincipals" -}}
- "{{ include "orbit.spiffePrincipalBase" .root }}/system/{{ .component }}"
{{- if not .spireEnabled }}
- "*/sa/{{ .serviceAccount }}-x-{{ include "orbit.systemNamespace" .root }}-x-vcluster"
{{- end }}
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

{{/*
orbit-triggers: the platform's front door for starting work (webhook ingestion,
the trigger management API, the manual fire endpoint, and the cron scheduler).
See docs/adr/2026-06-20-extended-trigger-types.md.
*/}}
{{- define "orbit.triggers.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-triggers
app.kubernetes.io/component: triggers
{{- end }}

{{- define "orbit.triggers.selectorLabels" -}}
app: orbit-triggers
{{- end }}

{{- define "orbit.triggers.imageTag" -}}
{{- .Values.triggers.image.tag | default .Chart.AppVersion }}
{{- end }}

{{- define "orbit.triggers.imagePullPolicy" -}}
{{- .Values.triggers.image.pullPolicy | default .Values.global.imagePullPolicy | default "IfNotPresent" }}
{{- end }}
