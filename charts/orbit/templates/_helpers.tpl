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
The orbit-system namespace.
*/}}
{{- define "orbit.systemNamespace" -}}
{{- .Values.platform.namespaces.system | default "orbit-system" }}
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
