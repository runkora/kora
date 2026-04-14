package operator

// buildSpireInjectionTemplate returns the Istio sidecar injection template that
// wires istio-proxy to use SPIRE for mTLS certificate delivery via the SPIFFE
// CSI driver. This is Orbit's opinionated wiring — users never write this.
func buildSpireInjectionTemplate() string {
	return `spec:
  containers:
    - name: istio-proxy
      env:
        - name: PILOT_CERT_PROVIDER
          value: "spire"
        - name: SPIFFE_ENDPOINT_SOCKET
          value: "unix:///run/secrets/workload-spiffe-uds/socket"
      volumeMounts:
        - name: workload-socket
          mountPath: /run/secrets/workload-spiffe-uds
          readOnly: true
  volumes:
    - name: workload-socket
      csi:
        driver: "csi.spiffe.io"
        readOnly: true
`
}

// buildOPAPolicyInjectionTemplate returns the Istio sidecar injection template
// that injects the OPA Envoy plugin sidecar into tenant agent pods.
//
// The OPA sidecar implements the envoy.service.auth.v3.Authorization gRPC
// protocol and listens on localhost:9191. The EnvoyFilter in the orbit chart
// wires Envoy's outbound ext_authz filter to this port on pods labeled
// orbit.io/opa-policy=enabled.
//
// The tenant ID is read at runtime from the pod annotation orbit.io/tenant-id
// (injected by the PodMutator webhook) so the OPA sidecar fetches the correct
// per-tenant bundle from the policy service.
//
// The orbit-opa-config ConfigMap is created in each tenant namespace by the
// TenantReconciler — it must be present before pods are scheduled.
func buildOPAPolicyInjectionTemplate(opaImage, opaLogLevel string) string {
	return `spec:
  containers:
  - name: opa
    image: ` + opaImage + `
    args:
      - "run"
      - "--server"
      - "--config-file=/config/config.yaml"
      - "--addr=0.0.0.0:8181"
      - "--diagnostic-addr=0.0.0.0:8282"
      - "--log-level=` + opaLogLevel + `"
    env:
      - name: ORBIT_TENANT_ID
        valueFrom:
          fieldRef:
            fieldPath: "metadata.annotations['orbit.io/tenant-id']"
    ports:
      - name: opa-grpc
        containerPort: 9191
        protocol: TCP
      - name: opa-http
        containerPort: 8181
        protocol: TCP
      - name: opa-diag
        containerPort: 8282
        protocol: TCP
    securityContext:
      runAsUser: 1337
    readinessProbe:
      httpGet:
        path: /health?bundles=true
        port: 8282
      initialDelaySeconds: 3
      periodSeconds: 5
      failureThreshold: 12
    livenessProbe:
      httpGet:
        path: /health
        port: 8282
      initialDelaySeconds: 10
      periodSeconds: 10
    volumeMounts:
      - name: orbit-opa-config
        mountPath: /config
        readOnly: true
  volumes:
  - name: orbit-opa-config
    configMap:
      name: orbit-opa-config
`
}

// buildIdentityProxyInjectionTemplate returns the Istio sidecar injection
// template that injects the identity-proxy container alongside istio-proxy.
// The identity-proxy intercepts outbound agent HTTP calls and attaches a
// platform Bearer token before forwarding.
//
// The image is resolved via orbitImageRef: built-in versioned constant first,
// then spec.imageRegistry prefix, then a bare "orbit-identity-proxy:latest" fallback.
func buildIdentityProxyInjectionTemplate(image string) string {
	return `spec:
  containers:
    - name: istio-proxy
      env:
        - name: SPIFFE_ENDPOINT_SOCKET
          value: "unix:///run/secrets/workload-spiffe-uds/socket"
      volumeMounts:
        - name: id-proxy-sock
          mountPath: /run/orbit
        - name: workload-socket
          mountPath: /run/secrets/workload-spiffe-uds
          readOnly: true
    - name: identity-proxy
      image: ` + image + `
      imagePullPolicy: Always
      env:
        - name: APP_ENV
          value: development
        - name: ORBIT_IDENTITY_SERVICE_URL
          value: "http://orbit-identity.orbit-system.svc.cluster.local"
        - name: SPIFFE_ENDPOINT_SOCKET
          value: "unix:///run/secrets/workload-spiffe-uds/socket"
      ports:
        - name: health
          containerPort: 9090
          protocol: TCP
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
        readOnlyRootFilesystem: true
        allowPrivilegeEscalation: false
        capabilities:
          drop: [ALL]
      readinessProbe:
        httpGet:
          path: /healthz
          port: 9090
        initialDelaySeconds: 5
        periodSeconds: 5
        timeoutSeconds: 3
        failureThreshold: 10
      resources:
        requests:
          cpu: 50m
          memory: 32Mi
        limits:
          cpu: 200m
          memory: 64Mi
      volumeMounts:
        - name: id-proxy-sock
          mountPath: /run/orbit
        - name: workload-socket
          mountPath: /run/secrets/workload-spiffe-uds
          readOnly: true
  volumes:
    - name: id-proxy-sock
      emptyDir: {}
    - name: workload-socket
      csi:
        driver: "csi.spiffe.io"
        readOnly: true
`
}
