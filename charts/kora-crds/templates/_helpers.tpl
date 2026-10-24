{{/*
Common labels for every CRD in this chart.
*/}}
{{- define "kora-crds.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: kora-crds
app.kubernetes.io/part-of: kora
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Printer columns every declarative kind shows: the Accepted and Ready conditions.
*/}}
{{- define "kora-crds.declaredColumns" -}}
- name: Accepted
  type: string
  jsonPath: .status.conditions[?(@.type=="Accepted")].status
- name: Ready
  type: string
  jsonPath: .status.conditions[?(@.type=="Ready")].status
{{- end }}

{{/*
The CEL rule that metadata.name is a DNS label.
*/}}
{{- define "kora-crds.dnsLabelName" -}}
- rule: "self.metadata.name.matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?$') && size(self.metadata.name) <= 63"
  message: metadata.name must be a DNS label.
{{- end }}

{{/*
Status properties every declarative kind reports. The operator writes them.
*/}}
{{- define "kora-crds.declaredStatus" -}}
id:
  type: string
  description: The id of the item the resource is projected to.
observedGeneration:
  type: integer
  format: int64
conditions:
  type: array
  x-kubernetes-list-type: map
  x-kubernetes-list-map-keys:
    - type
  items:
    type: object
    required:
      - type
      - status
      - lastTransitionTime
      - reason
      - message
    properties:
      type:
        type: string
      status:
        type: string
      observedGeneration:
        type: integer
        format: int64
      lastTransitionTime:
        type: string
        format: date-time
      reason:
        type: string
      message:
        type: string
{{- end }}
