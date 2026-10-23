{{/*
kora chart helpers. Component helpers take (dict "root" $ "name" "<values key>"),
the values key being the component's top-level block (identity, agents, ...).
*/}}

{{- define "kora.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels applied to every resource.
*/}}
{{- define "kora.labels" -}}
helm.sh/chart: {{ include "kora.chart" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: kora
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}

{{/*
Namespace kora runs in: global.systemNamespace when set, else the release namespace.
*/}}
{{- define "kora.systemNamespace" -}}
{{- .Values.global.systemNamespace | default .Release.Namespace }}
{{- end }}

{{- define "kora.trustDomain" -}}
{{- required "global.trustDomain must be set: the SPIFFE trust domain shared by SPIRE, Istio and kora" .Values.global.trustDomain }}
{{- end }}

{{/*
The external frontend host and the derived API host (ADR 2026-07-04).
*/}}
{{- define "kora.frontendHost" -}}
{{- required "frontendHost must be set: the external host of the frontend, e.g. orbit.example.com" .Values.frontendHost }}
{{- end }}

{{/*
Public hosts for OAuth discovery validation. Explicit publicHosts wins; otherwise
the API host and the frontend host, derived from frontendHost.
*/}}
{{- define "kora.publicHosts" -}}
{{- if .Values.publicHosts -}}
{{ join "," .Values.publicHosts }}
{{- else if .Values.frontendHost -}}
{{ printf "api.%s,%s" .Values.frontendHost .Values.frontendHost }}
{{- end -}}
{{- end }}

{{- define "kora.apiHost" -}}
api.{{ include "kora.frontendHost" . }}
{{- end }}

{{/*
In-cluster URL of a kora service by its Service name.
*/}}
{{- define "kora.serviceURL" -}}
http://{{ .name }}.{{ include "kora.systemNamespace" .root }}.svc.cluster.local
{{- end }}

{{/*
Istio Gateway reference every VirtualService binds to: kora's own gateway when
gateway.create is true, otherwise gateway.name.
*/}}
{{- define "kora.gatewayRef" -}}
{{- if .Values.gateway.create -}}
{{ include "kora.systemNamespace" . }}/{{ .Values.gateway.name }}
{{- else -}}
{{ .Values.gateway.name }}
{{- end -}}
{{- end }}

{{/*
Image reference for a component: <registry>/<repository>@<digest> when set
(released charts), otherwise <registry>/<repository>:<tag|appVersion>.
*/}}
{{- define "kora.image" -}}
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
{{- define "kora.imageRef" -}}
{{- $reg := .image.registry | default .root.Values.global.imageRegistry -}}
{{- $tag := .image.tag | default .root.Values.global.imageTag | default .root.Chart.AppVersion -}}
{{- if .image.digest -}}
{{ $reg }}/{{ .image.repository }}@{{ .image.digest }}
{{- else -}}
{{ $reg }}/{{ .image.repository }}:{{ $tag }}
{{- end -}}
{{- end }}

{{- define "kora.imagePullPolicy" -}}
{{- $img := (index .root.Values .name).image -}}
{{- $img.pullPolicy | default .root.Values.global.imagePullPolicy | default "IfNotPresent" }}
{{- end }}

{{- define "kora.imagePullSecrets" -}}
{{- with .Values.global.imagePullSecrets }}
imagePullSecrets:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
Scheduling for a component: component value when set, else the global default.
*/}}
{{- define "kora.scheduling" -}}
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
{{- define "kora.sandboxBaseImage" -}}
{{- .Values.agents.config.sandboxImage | default (printf "%s/kora-sandbox-base:%s" .Values.global.imageRegistry (.Values.global.imageTag | default .Chart.AppVersion)) -}}
{{- end }}

{{/*
The bundled sandbox images as the operator config carries them, keyed as in
agents.sandboxImages.bundled. The operator registers each enabled one in every
tenant namespace and removes the rest.
*/}}
{{- define "kora.bundledSandboxImages" -}}
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
{{- define "kora.sandboxWarmingMode" -}}
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
agents block places the kora-agents Deployment, which is not where sandboxes
run. priorityClassName is left to the caller, because the placeholder
Deployment sets its own.

With "spreadApp" and more than one replica, placeholders repel each other by
hostname, so replicas warm that many nodes rather than stacking on one. An
operator-supplied podAntiAffinity replaces it.
*/}}
{{- define "kora.sandboxWarmingScheduling" -}}
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
{{- define "kora.podSecurityContext" -}}
runAsNonRoot: true
runAsUser: {{ .uid }}
runAsGroup: {{ .uid }}
fsGroup: {{ .uid }}
seccompProfile:
  type: RuntimeDefault
{{- end }}

{{- define "kora.containerSecurityContext" -}}
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
details to third parties. An optional drainSecs keeps the sidecar serving that
long after SIGTERM so in-flight streams finish. Takes (dict "root" $ "drainSecs" <int>).
*/}}
{{- define "kora.spireProxyAnnotations" -}}
proxy.istio.io/config: |
  holdApplicationUntilProxyStarts: true
  proxyMetadata:
    PILOT_CERT_PROVIDER: spire
    SPIFFE_ENDPOINT_SOCKET: unix:///var/run/secrets/workload-spiffe-uds/socket
  proxyHeaders:
    metadataExchangeHeaders:
      mode: IN_MESH
  {{- with .drainSecs }}
  terminationDrainDuration: {{ . }}s
  {{- end }}
{{- end }}

{{/*
The SPIRE volume. Named workload-socket to shadow Istio's
own emptyDir of that name; the injector keeps the pod's definition, so the
CSI socket lands at the path the proxy checks for an external SDS socket.
*/}}
{{- define "kora.spireVolumes" -}}
- name: workload-socket
  csi:
    driver: csi.spiffe.io
    readOnly: true
{{- end }}

{{- define "kora.spireVolumeMount" -}}
- name: workload-socket
  mountPath: /run/secrets/workload-spiffe-uds
  readOnly: true
{{- end }}

{{/*
Env entries for a workload: a secretKeyRef for each platform Secret key it reads,
then its extraEnv. A key the extraEnv sets replaces the platform entry.
Takes (dict "root" $ "name" "identity" "keys" (list "DB_PASSWORD" ...)).
*/}}
{{- define "kora.platformEnv" -}}
{{- $extraEnv := (index .root.Values .name).extraEnv | default list }}
{{- $overridden := list }}
{{- range $extraEnv }}
{{- $overridden = append $overridden .name }}
{{- end }}
{{- range .keys }}
{{- if not (has . $overridden) }}
- name: {{ . }}
  valueFrom:
    secretKeyRef:
      name: {{ $.root.Values.config.platformSecretName }}
      key: {{ . }}
{{- end }}
{{- end }}
{{- with $extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Rendered service config files (docs/adr/2026-10-06-rendered-service-config-files.md).
A workload's modules map a config module (cmd/identity, internal/platform/storage)
to the YAML the loader merges last; <values key>.configOverrides merges over them.
These helpers take (dict "root" $ "name" "<values key>" "configMap" "<ConfigMap name>"
"modules" <dict>).
*/}}
{{- define "kora.configModules" -}}
{{- $overrides := (index .root.Values .name).configOverrides | default dict -}}
{{- mergeOverwrite (deepCopy .modules) (deepCopy $overrides) | toYaml -}}
{{- end }}

{{- define "kora.configMap" -}}
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .configMap }}
  namespace: {{ include "kora.systemNamespace" .root }}
  labels:
    {{- include "kora.labels" .root | nindent 4 }}
  {{- with .annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
data:
  {{- range $module, $content := include "kora.configModules" . | fromYaml }}
  {{ $module | replace "/" "." }}.yaml: |
    {{- toYaml $content | nindent 4 }}
  {{- end }}
{{- end }}

{{/* "true" when Redis TLS is on: config.redis.tls.enabled, on when unset. Take the root context. */}}
{{- define "kora.redisTLSEnabled" -}}
{{- $tls := .Values.config.redis.tls | default dict }}
{{- if or (not (hasKey $tls "enabled")) $tls.enabled }}true{{ end }}
{{- end }}

{{/* Path of the Redis CA bundle inside the pod, rendered into the storage config as caFile. */}}
{{- define "kora.redisCAFile" -}}
/etc/kora/redis-ca/ca.crt
{{- end }}

{{/*
Volume and mount for the Redis CA bundle from config.redis.tls.caSecret, for the
workloads that open a Redis client: identity, inference-gateway, triggers,
agents, policy-service, policy-gateway, the tool-gateway proxy and egress-gateway.
Take the root context.
*/}}
{{- define "kora.redisCAVolume" -}}
{{- $tls := .Values.config.redis.tls | default dict }}
{{- if and (include "kora.redisTLSEnabled" .) $tls.caSecret }}
- name: redis-ca
  secret:
    secretName: {{ $tls.caSecret }}
    items:
      - key: ca.crt
        path: ca.crt
{{- end }}
{{- end }}

{{- define "kora.redisCAVolumeMount" -}}
{{- $tls := .Values.config.redis.tls | default dict }}
{{- if and (include "kora.redisTLSEnabled" .) $tls.caSecret }}
- name: redis-ca
  mountPath: {{ include "kora.redisCAFile" . | dir }}
  readOnly: true
{{- end }}
{{- end }}

{{- define "kora.configVolume" -}}
- name: config-files
  configMap:
    name: {{ .configMap }}
    items:
      {{- range $module, $_ := include "kora.configModules" . | fromYaml }}
      - key: {{ $module | replace "/" "." }}.yaml
        path: {{ $module }}.yaml
      {{- end }}
{{- end }}

{{- define "kora.configVolumeMount" -}}
- name: config-files
  mountPath: /etc/kora/config
  readOnly: true
{{- end }}

{{/*
Config modules of the shared platform packages. Take the root context.
*/}}
{{- define "kora.observabilityConfigModule" -}}
{{- $obs := .Values.global.observability -}}
internal/platform/observability:
  logFormat: {{ .Values.global.logFormat | toJson }}
  otlpEndpoint: {{ $obs.otlpEndpoint | toJson }}
  tenantOtlpEndpoint: {{ $obs.otlpEndpoint | toJson }}
  {{- if ne (toString $obs.samplingRate) "" }}
  samplingRate: {{ $obs.samplingRate | float64 }}
  {{- end }}
{{- end }}

{{/*
Storage config module. Take the root context, or (dict "root" . "redisClient" true)
in the workloads that open a Redis client, which also get the CA file path.
*/}}
{{- define "kora.storageConfigModule" -}}
{{- $root := .root | default . }}
internal/platform/storage:
  postgres:
    host: {{ required "config.db.host must be set" $root.Values.config.db.host | toJson }}
    port: {{ $root.Values.config.db.port | int }}
    database: {{ $root.Values.config.db.name | toJson }}
    user: {{ $root.Values.config.db.user | toJson }}
    sslMode: {{ $root.Values.global.postgres.sslMode | toJson }}
    captureQueryText: {{ eq (toString $root.Values.global.postgres.captureQueryText) "true" }}
  redis:
    host: {{ required "config.redis.host must be set" $root.Values.config.redis.host | toJson }}
    port: {{ $root.Values.config.redis.port | int }}
    {{- $tls := $root.Values.config.redis.tls | default dict }}
    {{- $tlsOn := include "kora.redisTLSEnabled" $root }}
    tlsEnabled: {{ eq $tlsOn "true" }}
    {{- if $tlsOn }}
    {{- with $tls.serverName }}
    serverName: {{ . | toJson }}
    {{- end }}
    {{- if and .redisClient $tls.caSecret }}
    caFile: {{ include "kora.redisCAFile" $root | toJson }}
    {{- end }}
    {{- end }}
{{- end }}

{{- define "kora.tokenConfigModule" -}}
internal/platform/token:
  jwksURL: {{ printf "%s/api/v1/identity/.well-known/jwks.json" (include "kora.serviceURL" (dict "root" . "name" "kora-identity")) | toJson }}
{{- end }}

{{/*
Brand config module: the product name, for services that write text clients read. Take the root context.
*/}}
{{- define "kora.brandConfigModule" -}}
internal/platform/brand:
  productName: {{ include "kora.themeProductName" . | toJson }}
{{- end }}

{{/*
"true" when theme.document is set, empty otherwise. Take the root context.
*/}}
{{- define "kora.themeEnabled" -}}
{{- if .Values.theme.document }}true{{ end -}}
{{- end }}

{{/*
The backend product name: productName from theme.document, trimmed, or kora if unset. Take the root context.
Fails the render on a bad name, using the bounds in files/hades-product-name.json (`make hades-chart-rules`).
*/}}
{{- define "kora.themeProductName" -}}
{{- $name := "kora" -}}
{{- if include "kora.themeEnabled" . -}}
{{- $doc := fromYaml .Values.theme.document -}}
{{- if hasKey $doc "Error" -}}
{{- fail (printf "theme.document does not parse as a YAML map: %s" (get $doc "Error")) -}}
{{- end -}}
{{- if hasKey $doc "productName" -}}
{{- $p := get $doc "productName" -}}
{{- if kindIs "invalid" $p -}}
{{- fail "theme.document productName cannot be set to nothing; omit it to keep the default" -}}
{{- end -}}
{{- if not (kindIs "string" $p) -}}
{{- fail (printf "theme.document productName must be a string, got %s" (kindOf $p)) -}}
{{- end -}}
{{- $rule := .Files.Get "files/hades-product-name.json" | fromJson -}}
{{- $trimmed := trim $p -}}
{{- $length := len (regexFindAll "(?s)." $trimmed -1) -}}
{{- if or (lt $length (int $rule.minLength)) (gt $length (int $rule.maxLength)) -}}
{{- fail (printf "theme.document productName must be %d to %d characters after trimming, got %d" (int $rule.minLength) (int $rule.maxLength) $length) -}}
{{- end -}}
{{- if not (regexMatch $rule.pattern $trimmed) -}}
{{- fail (printf "theme.document productName is outside the Hades product-name bounds: it must match %s" $rule.pattern) -}}
{{- end -}}
{{- $name = $trimmed -}}
{{- end -}}
{{- end -}}
{{- $name -}}
{{- end }}

{{/*
Mounts the theme ConfigMap at /etc/kora/theme. Take the root context.
*/}}
{{- define "kora.themeVolumeMounts" -}}
- name: theme-document
  mountPath: /etc/kora/theme
  readOnly: true
{{- end }}

{{/*
The policy-service bundle endpoint the in-process evaluators poll.
*/}}
{{- define "kora.bundleEndpoint" -}}
{{ include "kora.serviceURL" (dict "root" . "name" "kora-policy-service") }}/api/v1/policy/bundles
{{- end }}

{{/*
The names of global.imagePullSecrets, as a JSON list.
*/}}
{{- define "kora.imagePullSecretNames" -}}
{{- $names := list -}}
{{- range .Values.global.imagePullSecrets }}{{ $names = append $names .name }}{{ end -}}
{{- $names | toJson -}}
{{- end }}

{{/*
Optional pod annotations, labels, env, volumes and mounts for a component.
Takes (dict "root" $ "name" "<values key>").
*/}}
{{- define "kora.podAnnotations" -}}
{{- with (index .root.Values .name).podAnnotations }}
{{- /* The chart owns the sidecar's proxy config: SPIRE certificates and IN_MESH peer metadata. */}}
{{- if hasKey . "proxy.istio.io/config" }}
{{- fail (printf "%s.podAnnotations cannot set proxy.istio.io/config: the chart owns the sidecar proxy config" $.name) }}
{{- end }}
{{- toYaml . }}
{{- end }}
{{- end }}

{{- define "kora.podLabels" -}}
{{- with (index .root.Values .name).podLabels }}
{{- toYaml . }}
{{- end }}
{{- end }}

{{- define "kora.extraEnv" -}}
{{- with (index .root.Values .name).extraEnv }}
{{- toYaml . }}
{{- end }}
{{- end }}

{{- define "kora.extraVolumes" -}}
{{- with (index .root.Values .name).extraVolumes }}
{{- toYaml . }}
{{- end }}
{{- end }}

{{- define "kora.extraVolumeMounts" -}}
{{- with (index .root.Values .name).extraVolumeMounts }}
{{- toYaml . }}
{{- end }}
{{- end }}

{{- define "kora.serviceAccountAnnotations" -}}
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
{{- define "kora.operator.labels" -}}
{{ include "kora.labels" . }}
app.kubernetes.io/name: kora-operator
app.kubernetes.io/component: operator
{{- end }}
{{- define "kora.operator.selectorLabels" -}}
app: kora-operator
{{- end }}

{{- define "kora.migrate.labels" -}}
{{ include "kora.labels" . }}
app.kubernetes.io/name: kora-migrate
app.kubernetes.io/component: migrate
{{- end }}

{{- define "kora.identity.labels" -}}
{{ include "kora.labels" . }}
app.kubernetes.io/name: kora-identity
app.kubernetes.io/component: identity
{{- end }}
{{- define "kora.identity.selectorLabels" -}}
app: kora-identity
{{- end }}

{{- define "kora.inferenceGateway.labels" -}}
{{ include "kora.labels" . }}
app.kubernetes.io/name: kora-inference-gateway
app.kubernetes.io/component: inference-gateway
{{- end }}
{{- define "kora.inferenceGateway.selectorLabels" -}}
app: kora-inference-gateway
{{- end }}

{{- define "kora.koraMcp.labels" -}}
{{ include "kora.labels" . }}
app.kubernetes.io/name: kora-mcp
app.kubernetes.io/component: kora-mcp
{{- end }}
{{- define "kora.koraMcp.selectorLabels" -}}
app: kora-mcp
{{- end }}

{{- define "kora.toolGateway.labels" -}}
{{ include "kora.labels" . }}
app.kubernetes.io/name: kora-tool-gateway
app.kubernetes.io/component: tool-gateway
{{- end }}
{{- define "kora.toolGateway.selectorLabels" -}}
app: kora-tool-gateway
{{- end }}

{{- define "kora.frontend.labels" -}}
{{ include "kora.labels" . }}
app.kubernetes.io/name: kora-frontend
app.kubernetes.io/component: frontend
{{- end }}
{{- define "kora.frontend.selectorLabels" -}}
app: kora-frontend
{{- end }}

{{- define "kora.policyService.labels" -}}
{{ include "kora.labels" . }}
app.kubernetes.io/name: kora-policy-service
app.kubernetes.io/component: policy-service
{{- end }}
{{- define "kora.policyService.selectorLabels" -}}
app: kora-policy-service
{{- end }}

{{- define "kora.policyGateway.labels" -}}
{{ include "kora.labels" . }}
app.kubernetes.io/name: kora-policy-gateway
app.kubernetes.io/component: policy-gateway
{{- end }}
{{- define "kora.policyGateway.selectorLabels" -}}
app: kora-policy-gateway
{{- end }}

{{- define "kora.egressGateway.labels" -}}
{{ include "kora.labels" . }}
app.kubernetes.io/name: kora-egress-gateway
app.kubernetes.io/component: egress-gateway
{{- end }}
{{- define "kora.egressGateway.selectorLabels" -}}
app: kora-egress-gateway
{{- end }}

{{- define "kora.opaGateway.labels" -}}
{{ include "kora.labels" . }}
app.kubernetes.io/name: kora-opa-gateway
app.kubernetes.io/component: opa-gateway
{{- end }}
{{- define "kora.opaGateway.selectorLabels" -}}
app: kora-opa-gateway
{{- end }}

{{- define "kora.agents.labels" -}}
{{ include "kora.labels" . }}
app.kubernetes.io/name: kora-agents
app.kubernetes.io/component: agents
{{- end }}
{{- define "kora.agents.selectorLabels" -}}
app: kora-agents
{{- end }}

{{- define "kora.triggers.labels" -}}
{{ include "kora.labels" . }}
app.kubernetes.io/name: kora-triggers
app.kubernetes.io/component: triggers
{{- end }}
{{- define "kora.triggers.selectorLabels" -}}
app: kora-triggers
{{- end }}

{{- define "kora.sandboxMount.labels" -}}
{{ include "kora.labels" . }}
app.kubernetes.io/name: kora-sandbox-mount
app.kubernetes.io/component: sandbox-mount
{{- end }}
{{- define "kora.sandboxMount.selectorLabels" -}}
app: kora-sandbox-mount
{{- end }}

{{/*
Forces x-forwarded-proto: https on API routes. Only needed behind a
TLS-terminating Ingress, where the gateway would otherwise stamp http and
OAuth metadata would reflect it.
*/}}
{{- define "kora.forceHttpsHeaders" -}}
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
{{- define "kora.gatewayWorkloadNamespace" -}}
{{- if .Values.gateway.create -}}
{{ include "kora.systemNamespace" . }}
{{- else -}}
{{ .Values.gateway.byo.namespace | default .Values.istio.namespace }}
{{- end -}}
{{- end }}

{{- define "kora.gatewayWorkloadSelector" -}}
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
{{- define "kora.collectorCIDRs" -}}
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

{{/*
Startup probe budget in seconds for the sandbox containers, shared by kora-agents
(sandbox-exec) and the operator webhook (identity-proxy, opa).
*/}}
{{- define "kora.sandboxStartupSeconds" -}}
{{- $s := int .Values.agents.sandboxStartupSeconds -}}
{{- if lt $s 1 }}{{ fail "agents.sandboxStartupSeconds must be at least 1" }}{{ end -}}
{{- $s -}}
{{- end }}
