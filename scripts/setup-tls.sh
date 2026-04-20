#!/usr/bin/env bash
# =============================================================================
# setup-tls.sh — Generate a TLS certificate for *.orbit.local.clearroute.io
#
# macOS (primary):
#   Uses mkcert to generate a browser-trusted wildcard certificate.
#   Installs the mkcert local CA into the system trust store (Keychain).
#
# Linux / other:
#   Falls back to a self-signed certificate via openssl. Dex and the rest of
#   the stack will work, but browsers will show a certificate warning.
#
# Windows / unsupported:
#   No-op with a warning. Run setup-tls manually in WSL.
#
# Output:
#   secrets/tls.crt       — certificate
#   secrets/tls.key       — private key (mode 644 for local Dex bind-mount)
#   secrets/mkcert-ca.pem — root CA (macOS + mkcert only)
#
# Idempotent — safe to run multiple times.
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

OS="$(uname -s)"

# ── Platform gate ─────────────────────────────────────────────────────────
# Windows (MINGW/MSYS/CYGWIN) — no-op. Use WSL instead.
case "${OS}" in
  MINGW*|MSYS*|CYGWIN*)
    warn "setup-tls.sh is not supported on Windows natively."
    warn "Run this script inside WSL, or run 'make setup-tls' from your WSL checkout."
    exit 0
    ;;
esac

# ── Existing certs — skip unless FORCE=1 ──────────────────────────────────
if [[ -f "${CERT_FILE}" && -f "${KEY_FILE}" && "${FORCE:-}" != "1" ]]; then
  ok "TLS certificate already exists (secrets/tls.crt). Skipping."
  ok "To regenerate, run: FORCE=1 make setup-tls"
  exit 0
fi

mkdir -p "${SECRETS_DIR}"

# ── macOS path: mkcert (browser-trusted) ─────────────────────────────────
if [[ "${OS}" == "Darwin" ]]; then
  info "Checking mkcert..."
  if ! command -v mkcert &>/dev/null; then
    info "Installing mkcert via Homebrew..."
    brew install mkcert nss
    ok "mkcert installed"
  else
    ok "mkcert already installed"
  fi

  # mkcert -install is idempotent — if the CA is already trusted it's a no-op.
  # On first run it writes a root CA to ~/Library/Application Support/mkcert and
  # adds it to the macOS Keychain (sudo required).
  info "Installing local CA (may prompt for sudo on first run)..."
  mkcert -install
  ok "Local CA trusted by system and browsers"

  info "Generating TLS certificate for orbit.local.clearroute.io + *.orbit.local.clearroute.io..."
  mkcert -cert-file "${CERT_FILE}" -key-file "${KEY_FILE}" "orbit.local.clearroute.io" "*.orbit.local.clearroute.io"
  chmod 644 "${KEY_FILE}"
  ok "Certificate written:"
  echo "    cert: ${CERT_FILE}"
  echo "    key:  ${KEY_FILE}"

  # Copy mkcert root CA — needed in-cluster so identity pods can verify Dex's TLS cert.
  CA_ROOT="$(mkcert -CAROOT)/rootCA.pem"
  CA_DEST="${SECRETS_DIR}/mkcert-ca.pem"
  cp "${CA_ROOT}" "${CA_DEST}"
  ok "Root CA copied to ${CA_DEST}"

  echo ""
  echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
  echo -e "${GREEN}  TLS certificate for *.orbit.local.clearroute.io generated      ${RESET}"
  echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
  echo ""
  echo "  Browsers on this machine will trust https://orbit.local.clearroute.io:8443"
  echo "  without certificate warnings."
  echo ""
  echo "  To load into the k3d cluster:"
  echo "    make cluster-tls-secret"
  echo ""
  exit 0
fi

# ── Linux / other: openssl self-signed fallback ──────────────────────────
if ! command -v openssl &>/dev/null; then
  die "Neither mkcert nor openssl found. Install openssl or mkcert to generate TLS certificates."
fi

warn "mkcert not available on ${OS} — generating self-signed certificate via openssl."
warn "Dex and the local stack will work, but browsers will show a certificate warning."
warn "For browser-trusted certs, install mkcert: https://github.com/FiloSottile/mkcert#installation"

info "Generating self-signed TLS certificate for orbit.local.clearroute.io + *.orbit.local.clearroute.io..."
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout "${KEY_FILE}" \
  -out "${CERT_FILE}" \
  -days 365 \
  -subj "/CN=orbit.local.clearroute.io" \
  -addext "subjectAltName=DNS:orbit.local.clearroute.io,DNS:*.orbit.local.clearroute.io" \
  2>/dev/null
chmod 644 "${KEY_FILE}"
ok "Self-signed certificate written:"
echo "    cert: ${CERT_FILE}"
echo "    key:  ${KEY_FILE}"

echo ""
echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
echo -e "${GREEN}  Self-signed TLS certificate generated            ${RESET}"
echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
echo ""
echo "  The local stack will work, but browsers will show a warning."
echo "  For browser-trusted certs on macOS, run: make setup-tls"
echo ""
