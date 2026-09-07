{{/*
orbit chart helpers. Component helpers take (dict "root" $ "name" "<values key>"),
the values key being the component's top-level block (identity, agents, ...).
*/}}

{{- define "orbit.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels applied to every resource.
*/}}
{{- define "orbit.labels" -}}
helm.sh/chart: {{ include "orbit.chart" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: orbit
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}

{{/*
Namespace Orbit runs in: global.systemNamespace when set, else the release namespace.
*/}}
{{- define "orbit.systemNamespace" -}}
{{- .Values.global.systemNamespace | default .Release.Namespace }}
{{- end }}

{{- define "orbit.trustDomain" -}}
{{- required "global.trustDomain must be set: the SPIFFE trust domain shared by SPIRE, Istio and Orbit" .Values.global.trustDomain }}
{{- end }}

{{/*
The external frontend host and the derived API host (ADR 2026-07-04).
*/}}
{{- define "orbit.frontendHost" -}}
{{- required "frontendHost must be set: the external host of the frontend, e.g. orbit.example.com" .Values.frontendHost }}
{{- end }}

{{/*
Public hosts for OAuth discovery validation. Explicit publicHosts wins; otherwise
the API host and the frontend host, derived from frontendHost.
*/}}
{{- define "orbit.publicHosts" -}}
{{- if .Values.publicHosts -}}
{{ join "," .Values.publicHosts }}
{{- else if .Values.frontendHost -}}
{{ printf "api.%s,%s" .Values.frontendHost .Values.frontendHost }}
{{- end -}}
{{- end }}

{{- define "orbit.apiHost" -}}
api.{{ include "orbit.frontendHost" . }}
{{- end }}

{{/*
In-cluster URL of an Orbit service by its Service name.
*/}}
{{- define "orbit.serviceURL" -}}
http://{{ .name }}.{{ include "orbit.systemNamespace" .root }}.svc.cluster.local
{{- end }}

{{/*
Istio Gateway reference every VirtualService binds to: Orbit's own gateway when
gateway.create is true, otherwise gateway.name.
*/}}
{{- define "orbit.gatewayRef" -}}
{{- if .Values.gateway.create -}}
{{ include "orbit.systemNamespace" . }}/{{ .Values.gateway.name }}
{{- else -}}
{{ .Values.gateway.name }}
{{- end -}}
{{- end }}

{{/*
Image reference for a component: <registry>/<repository>@<digest> when set
(released charts), otherwise <registry>/<repository>:<tag|appVersion>.
*/}}
{{/*
Service config value: the component's own config.<field> when the key is present,
otherwise the global default. Key presence is checked with hasKey so an explicit
false is honoured; sprig default would treat false as unset and use the global.
Takes (dict "root" $ "name" "<component>" "field" "<field>" "default" <global value>).
*/}}
{{- define "orbit.serviceConfig" -}}
{{- $c := (index .root.Values .name).config | default dict -}}
{{- if hasKey $c .field -}}
{{- index $c .field -}}
{{- else -}}
{{- .default -}}
{{- end -}}
{{- end }}

{{- define "orbit.image" -}}
{{- $img := (index .root.Values .name).image -}}
{{- $reg := $img.registry | default .root.Values.global.imageRegistry -}}
{{- $tag := $img.tag | default .root.Values.global.imageTag | default .root.Chart.AppVersion -}}
{{- if $img.digest -}}
{{ $reg }}/{{ $img.repository }}@{{ $img.digest }}
{{- else -}}
{{ $reg }}/{{ $img.repository }}:{{ $tag }}
{{- end -}}
{{- end }}

{{/*
Image reference for a nested image block (identityProxy.image, opaGateway.image).
Takes (dict "root" $ "image" <image map>).
*/}}
{{- define "orbit.imageRef" -}}
{{- $reg := .image.registry | default .root.Values.global.imageRegistry -}}
{{- $tag := .image.tag | default .root.Values.global.imageTag | default .root.Chart.AppVersion -}}
{{- if .image.digest -}}
{{ $reg }}/{{ .image.repository }}@{{ .image.digest }}
{{- else -}}
{{ $reg }}/{{ .image.repository }}:{{ $tag }}
{{- end -}}
{{- end }}

{{- define "orbit.imagePullPolicy" -}}
{{- $img := (index .root.Values .name).image -}}
{{- $img.pullPolicy | default .root.Values.global.imagePullPolicy | default "IfNotPresent" }}
{{- end }}

{{- define "orbit.imagePullSecrets" -}}
{{- with .Values.global.imagePullSecrets }}
imagePullSecrets:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
Scheduling for a component: component value when set, else the global default.
*/}}
{{- define "orbit.scheduling" -}}
{{- $c := index .root.Values .name -}}
{{- $g := .root.Values.global -}}
{{- with ($c.nodeSelector | default $g.nodeSelector) }}
nodeSelector:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with ($c.tolerations | default $g.tolerations) }}
tolerations:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with ($c.affinity | default $g.affinity) }}
affinity:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with ($c.topologySpreadConstraints | default $g.topologySpreadConstraints) }}
topologySpreadConstraints:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with ($c.priorityClassName | default $g.priorityClassName) }}
priorityClassName: {{ . }}
{{- end }}
{{- end }}

{{/*
Pod-level security context for the restricted Pod Security Standard. The
seccomp profile is inherited by istio-proxy and istio-validation, neither of
which sets its own. Takes (dict "uid" <int>).
*/}}
{{- define "orbit.podSecurityContext" -}}
runAsNonRoot: true
runAsUser: {{ .uid }}
runAsGroup: {{ .uid }}
fsGroup: {{ .uid }}
seccompProfile:
  type: RuntimeDefault
{{- end }}

{{- define "orbit.containerSecurityContext" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: {{ if hasKey . "readOnlyRootFilesystem" }}{{ .readOnlyRootFilesystem }}{{ else }}true{{ end }}
capabilities:
  drop: [ALL]
{{- end }}

{{/*
Pod annotations that route the Istio sidecar's certificate through SPIRE and
mount the SPIFFE workload socket, plus the identity-proxy socket when that
sidecar is present. Annotation-driven so it works on any conforming Istio,
with no custom injection template. Takes (dict "root" $ "identityProxy" <bool>).
*/}}
{{- define "orbit.spireProxyAnnotations" -}}
proxy.istio.io/config: |
  holdApplicationUntilProxyStarts: true
  proxyMetadata:
    PILOT_CERT_PROVIDER: spire
    SPIFFE_ENDPOINT_SOCKET: unix:///var/run/secrets/workload-spiffe-uds/socket
{{- if .identityProxy }}
sidecar.istio.io/userVolumeMount: '{"id-proxy-sock":{"mountPath":"/run/orbit"}}'
{{- end }}
{{- end }}

{{/*
Volumes for SPIRE (and the identity-proxy socket when requested).
Takes (dict "identityProxy" <bool>). Named workload-socket to shadow Istio's
own emptyDir of that name; the injector keeps the pod's definition, so the
CSI socket lands at the path the proxy checks for an external SDS socket.
*/}}
{{- define "orbit.spireVolumes" -}}
- name: workload-socket
  csi:
    driver: csi.spiffe.io
    readOnly: true
{{- if .identityProxy }}
- name: id-proxy-sock
  emptyDir: {}
{{- end }}
{{- end }}

{{- define "orbit.spireVolumeMount" -}}
- name: workload-socket
  mountPath: /run/secrets/workload-spiffe-uds
  readOnly: true
{{- end }}

{{/*
The identity-proxy sidecar: exchanges a service's SPIFFE identity for its own
platform token when calling other Orbit services. Takes (dict "root" $).
*/}}
{{- define "orbit.identityProxyContainer" -}}
{{- $ip := .root.Values.identityProxy -}}
- name: identity-proxy
  image: {{ include "orbit.imageRef" (dict "root" .root "image" $ip.image) | quote }}
  imagePullPolicy: {{ $ip.image.pullPolicy | default .root.Values.global.imagePullPolicy | default "IfNotPresent" }}
  env:
    - name: ORBIT_IDENTITY_SERVICE_URL
      value: {{ include "orbit.serviceURL" (dict "root" .root "name" "orbit-identity") | quote }}
    - name: SPIFFE_ENDPOINT_SOCKET
      value: "unix:///run/secrets/workload-spiffe-uds/socket"
  ports:
    - name: health
      containerPort: 9090
      protocol: TCP
  securityContext:
    {{- include "orbit.containerSecurityContext" (dict) | nindent 4 }}
  readinessProbe:
    httpGet:
      path: /healthz
      port: 9090
    initialDelaySeconds: 5
    periodSeconds: 5
    timeoutSeconds: 3
    failureThreshold: 10
  resources:
    {{- toYaml $ip.resources | nindent 4 }}
  volumeMounts:
    - name: id-proxy-sock
      mountPath: /run/orbit
    {{- include "orbit.spireVolumeMount" . | nindent 4 }}
{{- end }}

{{/*
envFrom for the platform ConfigMap and Secret every service reads.
*/}}
{{- define "orbit.platformEnvFrom" -}}
envFrom:
  - configMapRef:
      name: orbit-config
  - secretRef:
      name: {{ .Values.config.platformSecretName }}
{{- end }}

{{/*
Optional pod annotations, labels, env, volumes and mounts for a component.
Takes (dict "root" $ "name" "<values key>").
*/}}
{{- define "orbit.podAnnotations" -}}
{{- with (index .root.Values .name).podAnnotations }}
{{- toYaml . }}
{{- end }}
{{- end }}

{{- define "orbit.podLabels" -}}
{{- with (index .root.Values .name).podLabels }}
{{- toYaml . }}
{{- end }}
{{- end }}

{{- define "orbit.extraEnv" -}}
{{- with (index .root.Values .name).extraEnv }}
{{- toYaml . }}
{{- end }}
{{- end }}

{{- define "orbit.extraVolumes" -}}
{{- with (index .root.Values .name).extraVolumes }}
{{- toYaml . }}
{{- end }}
{{- end }}

{{- define "orbit.extraVolumeMounts" -}}
{{- with (index .root.Values .name).extraVolumeMounts }}
{{- toYaml . }}
{{- end }}
{{- end }}

{{- define "orbit.serviceAccountAnnotations" -}}
{{- with (index .root.Values .name).serviceAccount }}
{{- with .annotations }}
annotations:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Per-component labels. Selector labels back Service and Deployment matchLabels;
keep them stable.
*/}}
{{- define "orbit.operator.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-operator
app.kubernetes.io/component: operator
{{- end }}
{{- define "orbit.operator.selectorLabels" -}}
app: orbit-operator
{{- end }}

{{- define "orbit.identity.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-identity
app.kubernetes.io/component: identity
{{- end }}
{{- define "orbit.identity.selectorLabels" -}}
app: orbit-identity
{{- end }}

{{- define "orbit.inferenceGateway.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-inference-gateway
app.kubernetes.io/component: inference-gateway
{{- end }}
{{- define "orbit.inferenceGateway.selectorLabels" -}}
app: orbit-inference-gateway
{{- end }}

{{- define "orbit.orbitMcp.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-mcp
app.kubernetes.io/component: orbit-mcp
{{- end }}
{{- define "orbit.orbitMcp.selectorLabels" -}}
app: orbit-mcp
{{- end }}

{{- define "orbit.toolGateway.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-tool-gateway
app.kubernetes.io/component: tool-gateway
{{- end }}
{{- define "orbit.toolGateway.selectorLabels" -}}
app: orbit-tool-gateway
{{- end }}

{{- define "orbit.frontend.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-frontend
app.kubernetes.io/component: frontend
{{- end }}
{{- define "orbit.frontend.selectorLabels" -}}
app: orbit-frontend
{{- end }}

{{- define "orbit.policyService.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-policy-service
app.kubernetes.io/component: policy-service
{{- end }}
{{- define "orbit.policyService.selectorLabels" -}}
app: orbit-policy-service
{{- end }}

{{- define "orbit.policyGateway.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-policy-gateway
app.kubernetes.io/component: policy-gateway
{{- end }}
{{- define "orbit.policyGateway.selectorLabels" -}}
app: orbit-policy-gateway
{{- end }}

{{- define "orbit.egressGateway.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-egress-gateway
app.kubernetes.io/component: egress-gateway
{{- end }}
{{- define "orbit.egressGateway.selectorLabels" -}}
app: orbit-egress-gateway
{{- end }}

{{- define "orbit.opaGateway.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-opa-gateway
app.kubernetes.io/component: opa-gateway
{{- end }}
{{- define "orbit.opaGateway.selectorLabels" -}}
app: orbit-opa-gateway
{{- end }}

{{- define "orbit.agents.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-agents
app.kubernetes.io/component: agents
{{- end }}
{{- define "orbit.agents.selectorLabels" -}}
app: orbit-agents
{{- end }}

{{- define "orbit.triggers.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-triggers
app.kubernetes.io/component: triggers
{{- end }}
{{- define "orbit.triggers.selectorLabels" -}}
app: orbit-triggers
{{- end }}

{{/*
Forces x-forwarded-proto: https on API routes. Only needed behind a
TLS-terminating Ingress, where the gateway would otherwise stamp http and
OAuth metadata would reflect it.
*/}}
{{- define "orbit.forceHttpsHeaders" -}}
{{- if and .Values.gateway.create .Values.gateway.ingress.enabled }}
headers:
  request:
    set:
      x-forwarded-proto: https
{{- end }}
{{- end }}

{{/*
Where the Istio ingress gateway pods run and how to select them: Orbit's own
(gateway.create) in the system namespace, or an existing one per gateway.byo.
*/}}
{{- define "orbit.gatewayWorkloadNamespace" -}}
{{- if .Values.gateway.create -}}
{{ include "orbit.systemNamespace" . }}
{{- else -}}
{{ .Values.gateway.byo.namespace | default .Values.istio.namespace }}
{{- end -}}
{{- end }}

{{- define "orbit.gatewayWorkloadSelector" -}}
{{- if .Values.gateway.create -}}
app: {{ .Values.ingressGateway.name }}
{{- else -}}
{{ toYaml .Values.gateway.byo.selector }}
{{- end -}}
{{- end }}
