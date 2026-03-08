#!/usr/bin/env bash
# =============================================================================
# install-tools-cluster.sh — Install cluster tooling for local dev environment
#
# Installs (if not already present):
#   - k3d       >= 5.6   (local k3s cluster manager)
#   - kubectl   >= 1.29  (Kubernetes CLI)
#   - istioctl  >= 1.21  (Istio control plane CLI)
#   - helm      >= 3.14  (Kubernetes package manager)
# =============================================================================
set -euo pipefail

K3D_VERSION="v5.6.3"
KUBECTL_VERSION="v1.29.4"
ISTIO_VERSION="1.21.2"
HELM_VERSION="v3.14.4"

GREEN="\033[32m"
YELLOW="\033[33m"
BLUE="\033[36m"
RESET="\033[0m"

step()    { echo -e "\n${BLUE}▶ $*${RESET}"; }
ok()      { echo -e "${GREEN}✓ $*${RESET}"; }
already() { echo -e "${GREEN}✓ $* already installed — skipping${RESET}"; }

# Prefer GOPATH/bin when Go is available so cluster tools share the same
# PATH entry as Go tools.  Fall back to ~/.local/bin for machines without Go.
if command -v go &>/dev/null; then
  INSTALL_DIR="$(go env GOPATH)/bin"
else
  INSTALL_DIR="${HOME}/.local/bin"
fi
mkdir -p "${INSTALL_DIR}"
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

# Normalise arch strings
case "${ARCH}" in
  x86_64)  ARCH_K3D="amd64"; ARCH_KUBE="amd64"; ARCH_ISTIO="amd64"; ARCH_HELM="amd64" ;;
  aarch64|arm64) ARCH_K3D="arm64"; ARCH_KUBE="arm64"; ARCH_ISTIO="arm64"; ARCH_HELM="arm64" ;;
  *) echo "Unsupported architecture: ${ARCH}"; exit 1 ;;
esac

# ── k3d ──────────────────────────────────────────────────────────────────────
step "k3d ${K3D_VERSION}"
if command -v k3d &>/dev/null; then
  already "k3d $(k3d version | head -1)"
else
  curl -sSfL \
    "https://github.com/k3d-io/k3d/releases/download/${K3D_VERSION}/k3d-${OS}-${ARCH_K3D}" \
    -o "${INSTALL_DIR}/k3d"
  chmod +x "${INSTALL_DIR}/k3d"
  ok "k3d ${K3D_VERSION} installed → ${INSTALL_DIR}/k3d"
fi

# ── kubectl ───────────────────────────────────────────────────────────────────
step "kubectl ${KUBECTL_VERSION}"
if command -v kubectl &>/dev/null; then
  already "kubectl $(kubectl version --client -o yaml 2>/dev/null | grep -m1 gitVersion | awk '{print $2}')"
else
  curl -sSfL \
    "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/${OS}/${ARCH_KUBE}/kubectl" \
    -o "${INSTALL_DIR}/kubectl"
  chmod +x "${INSTALL_DIR}/kubectl"
  ok "kubectl ${KUBECTL_VERSION} installed → ${INSTALL_DIR}/kubectl"
fi

# ── istioctl ──────────────────────────────────────────────────────────────────
step "istioctl ${ISTIO_VERSION}"
if command -v istioctl &>/dev/null; then
  already "istioctl $(istioctl version --short 2>/dev/null | head -1)"
else
  ISTIO_OS="${OS}"
  # istio release tarballs use 'osx' not 'darwin'
  [ "${OS}" = "darwin" ] && ISTIO_OS="osx"
  ISTIO_TARBALL="istio-${ISTIO_VERSION}-${ISTIO_OS}-${ARCH_ISTIO}.tar.gz"
  curl -sSfL \
    "https://github.com/istio/istio/releases/download/${ISTIO_VERSION}/${ISTIO_TARBALL}" \
    | tar -xz -C /tmp
  mv "/tmp/istio-${ISTIO_VERSION}/bin/istioctl" "${INSTALL_DIR}/istioctl"
  rm -rf "/tmp/istio-${ISTIO_VERSION}"
  ok "istioctl ${ISTIO_VERSION} installed → ${INSTALL_DIR}/istioctl"
fi

# ── helm ──────────────────────────────────────────────────────────────────────
step "helm ${HELM_VERSION}"
if command -v helm &>/dev/null; then
  already "helm $(helm version --short 2>/dev/null)"
else
  curl -sSfL \
    "https://get.helm.sh/helm-${HELM_VERSION}-${OS}-${ARCH_HELM}.tar.gz" \
    | tar -xz -C /tmp
  mv "/tmp/${OS}-${ARCH_HELM}/helm" "${INSTALL_DIR}/helm"
  rm -rf "/tmp/${OS}-${ARCH_HELM}"
  ok "helm ${HELM_VERSION} installed → ${INSTALL_DIR}/helm"
fi

echo ""
echo -e "${GREEN}All cluster tools installed.${RESET}"
echo "  Run 'make dev-cluster' to bring up the local k3d cluster."
