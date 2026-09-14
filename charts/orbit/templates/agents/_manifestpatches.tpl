{{/*
Chart-generated manifest patches for the named knobs on
.Values.agents.runtimePods. Emitted into the orbit-agents-manifest-patches
ConfigMap keys; consumed by orbit-agents at startup
(docs/adr/2026-09-15-manifest-patches-configmap.md).

Each patch is either a Strategic Merge Patch (a partial k8s object) or a
JSON6902 patch (target + patch envelope). Filenames are numeric-prefixed so
the runtime's lex-sorted apply order is deterministic.
*/}}
{{- define "orbit.agents.generatedPatches" -}}
{{- $rp := .Values.agents.runtimePods -}}
{{- $ns := $rp.nodeSelector | default .Values.global.nodeSelector -}}
{{- $tol := $rp.tolerations | default .Values.global.tolerations -}}
{{- $pc := $rp.priorityClassName | default .Values.global.priorityClassName -}}

{{- with $ns }}
010-statefulset-nodeselector.yaml: |
  apiVersion: apps/v1
  kind: StatefulSet
  spec:
    template:
      spec:
        nodeSelector:
{{ toYaml . | indent 10 }}
011-pod-nodeselector.yaml: |
  apiVersion: v1
  kind: Pod
  spec:
    nodeSelector:
{{ toYaml . | indent 8 }}
{{- end }}

{{- with $tol }}
020-statefulset-tolerations.yaml: |
  apiVersion: apps/v1
  kind: StatefulSet
  spec:
    template:
      spec:
        tolerations:
{{ toYaml . | indent 10 }}
021-pod-tolerations.yaml: |
  apiVersion: v1
  kind: Pod
  spec:
    tolerations:
{{ toYaml . | indent 8 }}
{{- end }}

{{- with $pc }}
030-statefulset-priorityclass.yaml: |
  apiVersion: apps/v1
  kind: StatefulSet
  spec:
    template:
      spec:
        priorityClassName: {{ . | quote }}
031-pod-priorityclass.yaml: |
  apiVersion: v1
  kind: Pod
  spec:
    priorityClassName: {{ . | quote }}
{{- end }}

{{- with $rp.sandboxResources }}
040-sandbox-resources.yaml: |
  target:
    kind: Pod
    labelSelector: orbit.io/role=sandbox
  patch:
    - op: replace
      path: /spec/containers/0/resources
      value:
{{ toYaml . | indent 8 }}
{{- end }}

{{- with $rp.workspace.storageClassName }}
050-pvc-storageclass.yaml: |
  apiVersion: v1
  kind: PersistentVolumeClaim
  metadata:
    name: workspaces
  spec:
    storageClassName: {{ . | quote }}
{{- end }}

{{- with $rp.workspace.accessModes }}
051-pvc-accessmodes.yaml: |
  apiVersion: v1
  kind: PersistentVolumeClaim
  metadata:
    name: workspaces
  spec:
    accessModes:
{{ toYaml . | indent 6 }}
{{- end }}

{{- with $rp.serviceAccountAnnotations }}
060-serviceaccount-annotations.yaml: |
  apiVersion: v1
  kind: ServiceAccount
  metadata:
    annotations:
{{ toYaml . | indent 6 }}
{{- end }}
{{- end -}}
