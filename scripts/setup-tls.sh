#!/usr/bin/env bash
# =============================================================================
# setup-tls.sh — Generate a locally-trusted TLS certificate for *.orbit.test
#
# What this does:
#   1. Installs mkcert via Homebrew (if not present)
#   2. Installs the mkcert local CA into the system trust store (once per machine,
#      requires sudo for Keychain Access on macOS)
#   3. Generates a wildcard TLS certificate for orbit.test and *.orbit.test
#      into secrets/tls.crt + secrets/tls.key
#
# After this runs, the browser trusts https://orbit.test:8443 without warnings.
# The k3d cluster receives the cert via `make cluster-tls-secret`, which creates
# a Kubernetes TLS secret in istio-system for the Istio gateway.
#
# Idempotent — safe to run multiple times. Re-running regenerates the cert
# but reuses the existing local CA.
# =============================================================================
set -euo pipefail

GREEN="\033[32m"
YELLOW="\033[33m"
BLUE="\033[36m"
RED="\033[31m"
RESET="\033[0m"

info()  { echo -e "${BLUE}▶ $*${RESET}"; }
ok()    { echo -e "${GREEN}✓ $*${RESET}"; }
warn()  { echo -e "${YELLOW}⚠ $*${RESET}"; }
die()   { echo -e "${RED}✗ $*${RESET}" >&2; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
SECRETS_DIR="${REPO_ROOT}/secrets"
CERT_FILE="${SECRETS_DIR}/tls.crt"
KEY_FILE="${SECRETS_DIR}/tls.key"

# ── Install mkcert ────────────────────────────────────────────────────────
info "Checking mkcert..."
if ! command -v mkcert &>/dev/null; then
  if [[ "$(uname -s)" == "Darwin" ]]; then
    info "Installing mkcert via Homebrew..."
    brew install mkcert nss
    ok "mkcert installed"
  else
    die "mkcert not found. Install it: https://github.com/FiloSottile/mkcert#installation"
  fi
else
  ok "mkcert already installed"
fi

# ── Install local CA ─────────────────────────────────────────────────────
# mkcert -install is idempotent — if the CA is already trusted it's a no-op.
# On first run it writes a root CA to ~/Library/Application Support/mkcert and
# adds it to the macOS Keychain (sudo required).
info "Installing local CA (may prompt for sudo on first run)..."
mkcert -install
ok "Local CA trusted by system and browsers"

# ── Generate wildcard cert ────────────────────────────────────────────────
info "Generating TLS certificate for orbit.test + *.orbit.test..."
mkdir -p "${SECRETS_DIR}"
mkcert -cert-file "${CERT_FILE}" -key-file "${KEY_FILE}" "orbit.test" "*.orbit.test"
chmod 600 "${KEY_FILE}"
ok "Certificate written:"
echo "    cert: ${CERT_FILE}"
echo "    key:  ${KEY_FILE}"

# ── Copy mkcert root CA ──────────────────────────────────────────────────
# The root CA is needed in-cluster so identity pods can verify Dex's TLS cert.
CA_ROOT="$(mkcert -CAROOT)/rootCA.pem"
CA_DEST="${SECRETS_DIR}/mkcert-ca.pem"
cp "${CA_ROOT}" "${CA_DEST}"
ok "Root CA copied to ${CA_DEST}"

# ── Done ──────────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
echo -e "${GREEN}  TLS certificate for *.orbit.test generated      ${RESET}"
echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
echo ""
echo "  Browsers on this machine will trust https://orbit.test:8443"
echo "  without certificate warnings."
echo ""
echo "  To load into the k3d cluster:"
echo "    make cluster-tls-secret"
echo ""
