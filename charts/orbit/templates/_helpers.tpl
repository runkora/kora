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
Namespace kora runs in: global.systemNamespace when set, else the release namespace.
*/}}
{{- define "orbit.systemNamespace" -}}
{{- .Values.global.systemNamespace | default .Release.Namespace }}
{{- end }}

{{- define "orbit.trustDomain" -}}
{{- required "global.trustDomain must be set: the SPIFFE trust domain shared by SPIRE, Istio and kora" .Values.global.trustDomain }}
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
In-cluster URL of a kora service by its Service name.
*/}}
{{- define "orbit.serviceURL" -}}
http://{{ .name }}.{{ include "orbit.systemNamespace" .root }}.svc.cluster.local
{{- end }}

{{/*
Istio Gateway reference every VirtualService binds to: kora's own gateway when
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
The sandbox base image. The operator registers it in every tenant namespace as
the SandboxImage kora-base.
*/}}
{{- define "orbit.sandboxBaseImage" -}}
{{- .Values.agents.config.sandboxImage | default (printf "%s/kora-sandbox-base:%s" .Values.global.imageRegistry (.Values.global.imageTag | default .Chart.AppVersion)) -}}
{{- end }}

{{/*
The bundled sandbox images as the operator config carries them, keyed as in
agents.sandboxImages.bundled. The operator registers each enabled one in every
tenant namespace and removes the rest.
*/}}
{{- define "orbit.bundledSandboxImages" -}}
{{- $out := dict -}}
{{- range $key, $img := .Values.agents.sandboxImages.bundled -}}
{{- $ref := $img.image | default (printf "%s/kora-sandbox-%s:%s" $.Values.global.imageRegistry $key ($.Values.global.imageTag | default $.Chart.AppVersion)) -}}
{{- $_ := set $out $key (dict "enabled" (ne $img.enabled false) "warm" (eq $img.warm true) "image" $ref) -}}
{{- end -}}
{{- toJson $out -}}
{{- end }}

{{/*
The validated sandbox warming mode. A typo would otherwise render no warming at
all and look like it worked.
*/}}
{{- define "orbit.sandboxWarmingMode" -}}
{{- $mode := .Values.agents.sandboxWarming.mode | default "none" -}}
{{- if not (has $mode (list "none" "capacity")) -}}
{{- fail (printf "agents.sandboxWarming.mode is %q: expected none or capacity" $mode) -}}
{{- end -}}
{{- $mode -}}
{{- end }}

{{/*
Scheduling for a sandbox warm-start workload: agents.sandboxWarming when set,
else the sandbox placement, else the agents value, else the global default.
Takes (dict "root" $).

These pods exist to hold and warm the nodes sandboxes run on, so they follow
agents.workloads.sandbox and warm capacity is never configured twice. The
agents block places the orbit-agents Deployment, which is not where sandboxes
run. priorityClassName is left to the caller, because the placeholder
Deployment sets its own.

With "spreadApp" and more than one replica, placeholders repel each other by
hostname, so replicas warm that many nodes rather than stacking on one. An
operator-supplied podAntiAffinity replaces it.
*/}}
{{- define "orbit.sandboxWarmingScheduling" -}}
{{- $w := .root.Values.agents.sandboxWarming -}}
{{- $s := .root.Values.agents.workloads.sandbox -}}
{{- $c := .root.Values.agents -}}
{{- $g := .root.Values.global -}}
{{- with ($w.nodeSelector | default $s.nodeSelector | default $c.nodeSelector | default $g.nodeSelector) }}
nodeSelector:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with ($w.tolerations | default $s.tolerations | default $c.tolerations | default $g.tolerations) }}
tolerations:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- $affinity := ($w.affinity | default $s.affinity | default $c.affinity | default $g.affinity) | default dict -}}
{{- if and .spreadApp (gt ($w.replicas | int) 1) (not (hasKey $affinity "podAntiAffinity")) -}}
{{- $spread := dict "podAntiAffinity" (dict "requiredDuringSchedulingIgnoredDuringExecution" (list (dict "topologyKey" "kubernetes.io/hostname" "labelSelector" (dict "matchLabels" (dict "app" .spreadApp))))) -}}
{{- $affinity = mergeOverwrite (deepCopy $affinity) $spread -}}
{{- end -}}
{{- with $affinity }}
affinity:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with ($w.topologySpreadConstraints | default $s.topologySpreadConstraints | default $c.topologySpreadConstraints | default $g.topologySpreadConstraints) }}
topologySpreadConstraints:
  {{- toYaml . | nindent 2 }}
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
mount the SPIFFE workload socket. Annotation-driven so it works on any
conforming Istio, with no custom injection template. IN_MESH keeps Istio's
peer-metadata headers off calls that leave the mesh, so they cannot expose pod
details to third parties. Takes (dict "root" $).
*/}}
{{- define "orbit.spireProxyAnnotations" -}}
proxy.istio.io/config: |
  holdApplicationUntilProxyStarts: true
  proxyMetadata:
    PILOT_CERT_PROVIDER: spire
    SPIFFE_ENDPOINT_SOCKET: unix:///var/run/secrets/workload-spiffe-uds/socket
  proxyHeaders:
    metadataExchangeHeaders:
      mode: IN_MESH
{{- end }}

{{/*
The SPIRE volume. Named workload-socket to shadow Istio's
own emptyDir of that name; the injector keeps the pod's definition, so the
CSI socket lands at the path the proxy checks for an external SDS socket.
*/}}
{{- define "orbit.spireVolumes" -}}
- name: workload-socket
  csi:
    driver: csi.spiffe.io
    readOnly: true
{{- end }}

{{- define "orbit.spireVolumeMount" -}}
- name: workload-socket
  mountPath: /run/secrets/workload-spiffe-uds
  readOnly: true
{{- end }}

{{/*
envFrom for the platform Secret every service reads.
*/}}
{{- define "orbit.platformEnvFrom" -}}
envFrom:
  - secretRef:
      name: {{ .Values.config.platformSecretName }}
{{- end }}

{{/*
Rendered service config files (docs/adr/2026-10-06-rendered-service-config-files.md).
A workload's modules map a config module (cmd/identity, internal/platform/storage)
to the YAML the loader merges last; <values key>.configOverrides merges over them.
These helpers take (dict "root" $ "name" "<values key>" "configMap" "<ConfigMap name>"
"modules" <dict>).
*/}}
{{- define "orbit.configModules" -}}
{{- $overrides := (index .root.Values .name).configOverrides | default dict -}}
{{- mergeOverwrite (deepCopy .modules) (deepCopy $overrides) | toYaml -}}
{{- end }}

{{- define "orbit.configMap" -}}
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .configMap }}
  namespace: {{ include "orbit.systemNamespace" .root }}
  labels:
    {{- include "orbit.labels" .root | nindent 4 }}
  {{- with .annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
data:
  {{- range $module, $content := include "orbit.configModules" . | fromYaml }}
  {{ $module | replace "/" "." }}.yaml: |
    {{- toYaml $content | nindent 4 }}
  {{- end }}
{{- end }}

{{/*
The Redis CA bundle from config.redis.tls.caSecret, for the workloads that open
a Redis client: identity, inference-gateway and triggers. Take the root context.
*/}}
{{- define "orbit.redisCAFile" -}}
/etc/orbit/redis-ca/ca.crt
{{- end }}

{{- define "orbit.redisCAVolume" -}}
{{- $tls := .Values.config.redis.tls | default dict }}
{{- if and $tls.enabled $tls.caSecret }}
- name: redis-ca
  secret:
    secretName: {{ $tls.caSecret }}
    items:
      - key: ca.crt
        path: ca.crt
{{- end }}
{{- end }}

{{- define "orbit.redisCAVolumeMount" -}}
{{- $tls := .Values.config.redis.tls | default dict }}
{{- if and $tls.enabled $tls.caSecret }}
- name: redis-ca
  mountPath: {{ include "orbit.redisCAFile" . | dir }}
  readOnly: true
{{- end }}
{{- end }}

{{- define "orbit.configVolume" -}}
- name: config-files
  configMap:
    name: {{ .configMap }}
    items:
      {{- range $module, $_ := include "orbit.configModules" . | fromYaml }}
      - key: {{ $module | replace "/" "." }}.yaml
        path: {{ $module }}.yaml
      {{- end }}
{{- end }}

{{- define "orbit.configVolumeMount" -}}
- name: config-files
  mountPath: /etc/orbit/config
  readOnly: true
{{- end }}

{{/*
Config modules of the shared platform packages. Take the root context.
*/}}
{{- define "orbit.observabilityConfigModule" -}}
{{- $obs := .Values.global.observability -}}
internal/platform/observability:
  logFormat: {{ .Values.global.logFormat | toJson }}
  otlpEndpoint: {{ $obs.otlpEndpoint | toJson }}
  tenantOtlpEndpoint: {{ $obs.otlpEndpoint | toJson }}
  {{- if ne (toString $obs.samplingRate) "" }}
  samplingRate: {{ $obs.samplingRate | float64 }}
  {{- end }}
{{- end }}

{{- define "orbit.storageConfigModule" -}}
internal/platform/storage:
  postgres:
    host: {{ required "config.db.host must be set" .Values.config.db.host | toJson }}
    port: {{ .Values.config.db.port | int }}
    database: {{ .Values.config.db.name | toJson }}
    user: {{ .Values.config.db.user | toJson }}
    sslMode: {{ .Values.global.postgres.sslMode | toJson }}
    captureQueryText: {{ eq (toString .Values.global.postgres.captureQueryText) "true" }}
  redis:
    host: {{ required "config.redis.host must be set" .Values.config.redis.host | toJson }}
    port: {{ .Values.config.redis.port | int }}
    {{- $tls := .Values.config.redis.tls | default dict }}
    tlsEnabled: {{ eq (toString $tls.enabled) "true" }}
    {{- if $tls.enabled }}
    {{- with $tls.serverName }}
    serverName: {{ . | toJson }}
    {{- end }}
    {{- if $tls.caSecret }}
    caFile: {{ include "orbit.redisCAFile" . | toJson }}
    {{- end }}
    {{- end }}
{{- end }}

{{- define "orbit.tokenConfigModule" -}}
internal/platform/token:
  jwksURL: {{ printf "%s/api/v1/identity/.well-known/jwks.json" (include "orbit.serviceURL" (dict "root" . "name" "orbit-identity")) | toJson }}
{{- end }}

{{/*
The policy-service bundle endpoint the in-process evaluators poll.
*/}}
{{- define "orbit.bundleEndpoint" -}}
{{ include "orbit.serviceURL" (dict "root" . "name" "orbit-policy-service") }}/api/v1/policy/bundles
{{- end }}

{{/*
The names of global.imagePullSecrets, as a JSON list.
*/}}
{{- define "orbit.imagePullSecretNames" -}}
{{- $names := list -}}
{{- range .Values.global.imagePullSecrets }}{{ $names = append $names .name }}{{ end -}}
{{- $names | toJson -}}
{{- end }}

{{/*
Optional pod annotations, labels, env, volumes and mounts for a component.
Takes (dict "root" $ "name" "<values key>").
*/}}
{{- define "orbit.podAnnotations" -}}
{{- with (index .root.Values .name).podAnnotations }}
{{- /* The chart owns the sidecar's proxy config: SPIRE certificates and IN_MESH peer metadata. */}}
{{- if hasKey . "proxy.istio.io/config" }}
{{- fail (printf "%s.podAnnotations cannot set proxy.istio.io/config: the chart owns the sidecar proxy config" $.name) }}
{{- end }}
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

{{- define "orbit.migrate.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-migrate
app.kubernetes.io/component: migrate
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

{{- define "orbit.sandboxMount.labels" -}}
{{ include "orbit.labels" . }}
app.kubernetes.io/name: orbit-sandbox-mount
app.kubernetes.io/component: sandbox-mount
{{- end }}
{{- define "orbit.sandboxMount.selectorLabels" -}}
app: orbit-sandbox-mount
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
Where the Istio ingress gateway pods run and how to select them: kora's own
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

{{/*
The networkPolicy.externalServices CIDRs serving an out-of-cluster collector's
port, comma-separated. Empty when the collector runs in the cluster or no
endpoint is set. The port parse matches the collector rule in netpols.yaml.
*/}}
{{- define "orbit.collectorCIDRs" -}}
{{- $obs := .Values.global.observability -}}
{{- if and $obs.otlpEndpoint (not $obs.collectorNamespace) -}}
{{- $port := trimPrefix ":" (regexFind ":[0-9]+$" (trimSuffix "/" $obs.otlpEndpoint)) | default "4317" -}}
{{- $cidrs := list -}}
{{- range .Values.networkPolicy.externalServices -}}
{{- $svc := . -}}
{{- range .ports -}}
{{- if eq (toString .) $port -}}
{{- $cidrs = append $cidrs $svc.cidr -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $cidrs | uniq | join "," -}}
{{- end -}}
{{- end -}}
