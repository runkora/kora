{{/*
Expand the name of the chart.
*/}}
{{- define "orbit-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "orbit-operator.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
app.kubernetes.io/name: orbit-operator
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Values.image.tag | default .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "orbit-operator.selectorLabels" -}}
app.kubernetes.io/name: orbit-operator
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Image tag — falls back to chart appVersion.
*/}}
{{- define "orbit-operator.imageTag" -}}
{{- .Values.image.tag | default .Chart.AppVersion }}
{{- end }}

{{/*
ServiceAccount name.
*/}}
{{- define "orbit-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- .Values.serviceAccount.name | default "orbit-operator" }}
{{- else }}
{{- .Values.serviceAccount.name }}
{{- end }}
{{- end }}
