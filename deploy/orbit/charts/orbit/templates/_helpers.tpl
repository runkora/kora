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
Identity service labels — applied to the Deployment and its pod template.
*/}}
{{- define "orbit-platform.identity.labels" -}}
{{ include "orbit-platform.labels" . }}
app.kubernetes.io/name: orbit-identity
app.kubernetes.io/component: identity
{{- end }}

{{/*
Identity service selector labels — must be stable; used by the Service selector
and the Deployment matchLabels. Do not add mutable labels here.
*/}}
{{- define "orbit-platform.identity.selectorLabels" -}}
app: orbit-identity
{{- end }}

{{/*
Resolve the image tag for a service. Uses .Values.identity.image.tag when set,
otherwise falls back to .Chart.AppVersion. This ensures the chart always deploys
a known version even when the tag is omitted from values.
*/}}
{{- define "orbit-platform.identity.imageTag" -}}
{{- .Values.identity.image.tag | default .Chart.AppVersion }}
{{- end }}

{{/*
Resolve the imagePullPolicy for the identity service.
Pod-level override → global default → IfNotPresent.
*/}}
{{- define "orbit-platform.identity.imagePullPolicy" -}}
{{- .Values.identity.image.pullPolicy | default .Values.global.imagePullPolicy | default "IfNotPresent" }}
{{- end }}

{{/*
The effective trust domain — used in DestinationRule SAN URIs.
Sourced from global.trustDomain so it stays consistent with orbit-platform.
*/}}
{{- define "orbit-platform.trustDomain" -}}
{{- required "global.trustDomain must be set" .Values.global.trustDomain }}
{{- end }}

{{/*
The orbit-system namespace.
*/}}
{{- define "orbit-platform.systemNamespace" -}}
{{- .Values.identity.namespace | default .Values.global.namespaces.system | default "orbit-system" }}
{{- end }}

{{/*
The istio-system namespace.
*/}}
{{- define "orbit-platform.istioNamespace" -}}
{{- .Values.identityProxy.namespace | default .Values.global.namespaces.istio | default "istio-system" }}
{{- end }}

{{/*
The identity ServiceAccount name.
*/}}
{{- define "orbit-platform.identity.serviceAccountName" -}}
{{- .Values.identity.serviceAccount.name | default "orbit-identity" }}
{{- end }}

{{/*
The identity non-secret config ConfigMap name.
*/}}
{{- define "orbit-platform.identity.configMapName" -}}
{{- if .Values.identity.infrastructureConfig.create -}}
{{- .Values.identity.infrastructureConfig.name | default "orbit-config" -}}
{{- else -}}
{{- .Values.identity.existingConfigMap -}}
{{- end -}}
{{- end }}

{{/*
The identity tenant registry ConfigMap name.
*/}}
{{- define "orbit-platform.identity.tenantsConfigMapName" -}}
{{- if .Values.identity.tenantsConfig.create -}}
{{- .Values.identity.tenantsConfig.name | default "orbit-tenants" -}}
{{- else -}}
{{- .Values.identity.existingTenantsConfigMap -}}
{{- end -}}
{{- end }}

{{/*
Serialized Istio injection template list.
*/}}
{{- define "orbit-platform.identity.injectTemplates" -}}
{{- join "," .Values.identity.mesh.templates }}
{{- end }}

{{/*
Serialized excluded outbound ports for the identity sidecar.
*/}}
{{- define "orbit-platform.identity.excludeOutboundPorts" -}}
{{- $ports := list -}}
{{- range .Values.identity.mesh.excludeOutboundPorts }}
{{- $ports = append $ports (printf "%v" .) -}}
{{- end }}
{{- join "," $ports }}
{{- end }}
