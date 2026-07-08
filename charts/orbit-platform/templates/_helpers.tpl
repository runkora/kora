{{/*
orbit-platform chart helpers
*/}}

{{/*
Expand the name of the chart.
*/}}
{{- define "orbit-platform.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "orbit-platform.fullname" -}}
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
{{- define "orbit-platform.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels applied to all resources in this chart.
*/}}
{{- define "orbit-platform.labels" -}}
helm.sh/chart: {{ include "orbit-platform.chart" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: orbit
{{- end }}

{{/*
The effective trust domain — used in ClusterSPIFFEID templates and Istio config.
Always sourced from global.trustDomain so it stays consistent across charts.
*/}}
{{- define "orbit-platform.trustDomain" -}}
{{- required "global.trustDomain must be set — it must match across orbit-platform and orbit chart values" .Values.global.trustDomain }}
{{- end }}

{{/*
The orbit-system namespace name.
*/}}
{{- define "orbit-platform.systemNamespace" -}}
{{- .Values.namespaces.system | default "orbit-system" }}
{{- end }}

{{/*
The external frontend/BFF host (no scheme, no port), e.g. orbit.example.com. The
unified backend API host is api.<frontendHost> (north-south convention, ADR
2026-07-04). Must match the orbit chart's frontendHost. Set per environment —
never hardcode a domain in the chart.
*/}}
{{- define "orbit-platform.frontendHost" -}}
{{- required "frontendHost must be set — the external host of the frontend/BFF, e.g. orbit.example.com (must match the orbit chart)" .Values.frontendHost }}
{{- end }}

{{/*
The unified backend/data-plane API host: api.<frontendHost>.
*/}}
{{- define "orbit-platform.apiHost" -}}
api.{{ include "orbit-platform.frontendHost" . }}
{{- end }}
