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
