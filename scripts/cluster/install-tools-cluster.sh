#!/usr/bin/env bash
# =============================================================================
# install-tools-cluster.sh — Install cluster tooling for local dev environment
#
# Installs (if not already present at the correct version):
#   - k3d        >= 5.6   (local k3s cluster manager)
#   - kubectl    >= 1.29  (Kubernetes CLI)
#   - helm       >= 3.14  (Kubernetes package manager)
#   - helmfile   >= 0.169 (Helm orchestrator — escape-hatch reference path; operator
#                          is the primary install mechanism for local dev)
#   - helm-diff  >= 3.9   (Helm plugin — required by helmfile diff)
#
# Versions are pinned in scripts/cluster/versions.env — change them there.
#
# Note: istioctl is NOT required. SPIRE and Istio are installed by the orbit
# operator (charts/orbit-operator) via the Helm Go SDK — no CLI tooling needed.
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/cluster/versions.env
source "${SCRIPT_DIR}/versions.env"

GREEN="\033[32m"
YELLOW="\033[33m"
BLUE="\033[36m"
RESET="\033[0m"

step()    { echo -e "\n${BLUE}▶ $*${RESET}"; }
ok()      { echo -e "${GREEN}✓ $*${RESET}"; }
warn()    { echo -e "${YELLOW}⚠ $*${RESET}"; }
already() { echo -e "${GREEN}✓ $* already installed at correct version — skipping${RESET}"; }

# Prefer GOPATH/bin when Go is available so cluster tools share the same
# PATH entry as Go tools.  Fall back to ~/.local/bin for machines without Go.
if command -v go >/dev/null; then
  INSTALL_DIR="$(go env GOPATH)/bin"
else
  INSTALL_DIR="${HOME}/.local/bin"
fi
mkdir -p "${INSTALL_DIR}"

# uname -s returns 'Darwin' on macOS — lower-cased for download URLs.
# Note: for release assets that use 'osx' instead of 'darwin', see helm below.
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

# Normalise arch strings to the values used by each tool's release assets.
case "${ARCH}" in
  x86_64)        ARCH_K3D="amd64"; ARCH_KUBE="amd64"; ARCH_HELM="amd64" ;;
  aarch64|arm64) ARCH_K3D="arm64"; ARCH_KUBE="arm64"; ARCH_HELM="arm64" ;;
  *) echo "Unsupported architecture: ${ARCH}"; exit 1 ;;
esac

# ── k3d ──────────────────────────────────────────────────────────────────────
step "k3d ${K3D_VERSION}"
if command -v k3d >/dev/null; then
  INSTALLED="$(k3d version 2>&1 | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)"
  if [ "${INSTALLED}" = "${K3D_VERSION}" ]; then
    already "k3d ${INSTALLED}"
  else
    warn "k3d ${INSTALLED} is installed but ${K3D_VERSION} is required — reinstalling"
    curl -sSfL \
      "https://github.com/k3d-io/k3d/releases/download/${K3D_VERSION}/k3d-${OS}-${ARCH_K3D}" \
      -o "${INSTALL_DIR}/k3d"
    chmod +x "${INSTALL_DIR}/k3d"
    ok "k3d ${K3D_VERSION} installed → ${INSTALL_DIR}/k3d"
  fi
else
  curl -sSfL \
    "https://github.com/k3d-io/k3d/releases/download/${K3D_VERSION}/k3d-${OS}-${ARCH_K3D}" \
    -o "${INSTALL_DIR}/k3d"
  chmod +x "${INSTALL_DIR}/k3d"
  ok "k3d ${K3D_VERSION} installed → ${INSTALL_DIR}/k3d"
fi

# ── kubectl ───────────────────────────────────────────────────────────────────
step "kubectl ${KUBECTL_VERSION}"
if command -v kubectl >/dev/null; then
  INSTALLED="$(kubectl version --client -o yaml 2>/dev/null | grep -m1 gitVersion | awk '{print $2}' || true)"
  if [ "${INSTALLED}" = "${KUBECTL_VERSION}" ]; then
    already "kubectl ${INSTALLED}"
  else
    warn "kubectl ${INSTALLED} is installed but ${KUBECTL_VERSION} is required — reinstalling"
    curl -sSfL \
      "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/${OS}/${ARCH_KUBE}/kubectl" \
      -o "${INSTALL_DIR}/kubectl"
    chmod +x "${INSTALL_DIR}/kubectl"
    ok "kubectl ${KUBECTL_VERSION} installed → ${INSTALL_DIR}/kubectl"
  fi
else
  curl -sSfL \
    "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/${OS}/${ARCH_KUBE}/kubectl" \
    -o "${INSTALL_DIR}/kubectl"
  chmod +x "${INSTALL_DIR}/kubectl"
  ok "kubectl ${KUBECTL_VERSION} installed → ${INSTALL_DIR}/kubectl"
fi

# ── helm ──────────────────────────────────────────────────────────────────────
# Used to install both Istio (istio/base + istio/istiod) and SPIRE (spiffe/spire).
# Helm release assets use 'darwin' on macOS (consistent with uname -s output).
# Note: if Istio v1.22+ renames its asset prefix from 'osx' to 'darwin', only
# the istio download URL would need updating — helm is unaffected.
step "helm ${HELM_VERSION}"
if command -v helm >/dev/null; then
  INSTALLED="$(helm version --short 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)"
  if [ "${INSTALLED}" = "${HELM_VERSION}" ]; then
    already "helm ${INSTALLED}"
  else
    warn "helm ${INSTALLED} is installed but ${HELM_VERSION} is required — reinstalling"
    curl -sSfL \
      "https://get.helm.sh/helm-${HELM_VERSION}-${OS}-${ARCH_HELM}.tar.gz" \
      | tar -xz -C /tmp
    mv "/tmp/${OS}-${ARCH_HELM}/helm" "${INSTALL_DIR}/helm"
    rm -rf "/tmp/${OS}-${ARCH_HELM}"
    ok "helm ${HELM_VERSION} installed → ${INSTALL_DIR}/helm"
  fi
else
  curl -sSfL \
    "https://get.helm.sh/helm-${HELM_VERSION}-${OS}-${ARCH_HELM}.tar.gz" \
    | tar -xz -C /tmp
  mv "/tmp/${OS}-${ARCH_HELM}/helm" "${INSTALL_DIR}/helm"
  rm -rf "/tmp/${OS}-${ARCH_HELM}"
  ok "helm ${HELM_VERSION} installed → ${INSTALL_DIR}/helm"
fi

# ── helmfile ──────────────────────────────────────────────────────────────────
# Helmfile is retained as an escape-hatch / reference install path. The primary
# local dev workflow uses the orbit-operator (make cluster-operator). Helmfile
# can be used to manually install or inspect individual Helm releases when
# bypassing the operator is needed for debugging.
step "helmfile ${HELMFILE_VERSION}"
if command -v helmfile >/dev/null; then
  INSTALLED="$(helmfile version 2>/dev/null | grep -E '^\s+Version' | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)"
  if [ "${INSTALLED}" = "${HELMFILE_VERSION#v}" ]; then
    already "helmfile ${INSTALLED}"
  else
    warn "helmfile ${INSTALLED} is installed but ${HELMFILE_VERSION#v} is required — reinstalling"
    curl -sSfL \
      "https://github.com/helmfile/helmfile/releases/download/${HELMFILE_VERSION}/helmfile_${HELMFILE_VERSION#v}_${OS}_${ARCH_HELM}.tar.gz" \
      | tar -xz -C /tmp helmfile
    mv "/tmp/helmfile" "${INSTALL_DIR}/helmfile"
    ok "helmfile ${HELMFILE_VERSION} installed → ${INSTALL_DIR}/helmfile"
  fi
else
  curl -sSfL \
    "https://github.com/helmfile/helmfile/releases/download/${HELMFILE_VERSION}/helmfile_${HELMFILE_VERSION#v}_${OS}_${ARCH_HELM}.tar.gz" \
    | tar -xz -C /tmp helmfile
  mv "/tmp/helmfile" "${INSTALL_DIR}/helmfile"
  ok "helmfile ${HELMFILE_VERSION} installed → ${INSTALL_DIR}/helmfile"
fi

# ── helm-diff plugin ──────────────────────────────────────────────────────────
# Required by helmfile for `helmfile diff` (escape-hatch debugging).
# Installs as a Helm plugin rather than a standalone binary.
# If an incompatible version is present (e.g. built for a newer Helm), the
# plugin directory must be force-removed because `helm plugin remove` fails
# when the plugin.yaml is malformed for the current Helm version.
step "helm-diff plugin ${HELM_DIFF_VERSION}"
HELM_DIFF_PLUGIN_DIR="${HOME}/Library/helm/plugins/helm-diff"
# On Linux, helm plugins live under ${HOME}/.local/share/helm/plugins/
if [ "${OS}" = "linux" ]; then
  HELM_DIFF_PLUGIN_DIR="${HOME}/.local/share/helm/plugins/helm-diff"
fi
INSTALLED_DIFF=""
if helm diff version &>/dev/null 2>&1; then
  INSTALLED_DIFF="$(helm diff version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)"
fi
if [ "${INSTALLED_DIFF}" = "${HELM_DIFF_VERSION#v}" ]; then
  already "helm-diff ${INSTALLED_DIFF}"
else
  if [ -n "${INSTALLED_DIFF}" ]; then
    warn "helm-diff ${INSTALLED_DIFF} is installed but ${HELM_DIFF_VERSION#v} is required — reinstalling"
  fi
  # Force-remove the plugin directory — `helm plugin remove` fails when the
  # plugin manifest is incompatible with the current Helm binary.
  rm -rf "${HELM_DIFF_PLUGIN_DIR}"
  helm plugin install https://github.com/databus23/helm-diff \
    --version "${HELM_DIFF_VERSION}" 2>&1 | grep -v "^$" || true
  ok "helm-diff ${HELM_DIFF_VERSION} installed"
fi

echo ""
echo -e "${GREEN}All cluster tools installed.${RESET}"
echo "  Run 'make dev-cluster' to bring up the local k3d cluster."
