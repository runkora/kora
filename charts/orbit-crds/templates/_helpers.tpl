{{/*
Common labels for every CRD in this chart.
*/}}
{{- define "orbit-crds.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: orbit-crds
app.kubernetes.io/part-of: orbit
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}
