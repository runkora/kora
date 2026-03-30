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
