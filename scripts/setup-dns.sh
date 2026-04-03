#!/usr/bin/env bash
# =============================================================================
# setup-dns.sh — Configure local DNS for *.orbit.test on macOS
#
# What this does:
#   1. Installs dnsmasq via Homebrew (if not present)
#   2. Configures dnsmasq to resolve *.orbit.test → 127.0.0.1
#   3. Tells macOS to use dnsmasq for .orbit.test queries
#      (writes /etc/resolver/orbit.test — requires sudo once)
#
# After this runs, any *.orbit.test hostname resolves to 127.0.0.1 on your
# Mac without touching /etc/hosts. The k3d cluster gets a matching CoreDNS
# entry during `make dev-cluster` pointing to the Docker bridge gateway.
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

# ── Platform check ─────────────────────────────────────────────────────────
if [[ "$(uname -s)" != "Darwin" ]]; then
  warn "setup-dns.sh is macOS-only. Skipping on $(uname -s)."
  exit 0
fi

# ── Homebrew prefix (Apple Silicon vs Intel) ───────────────────────────────
if [[ -d /opt/homebrew ]]; then
  BREW_PREFIX=/opt/homebrew
elif [[ -d /usr/local/Homebrew ]]; then
  BREW_PREFIX=/usr/local
else
  die "Homebrew not found. Install it first: https://brew.sh"
fi

DNSMASQ_CONF_DIR="${BREW_PREFIX}/etc/dnsmasq.d"
ORBIT_CONF="${DNSMASQ_CONF_DIR}/orbit.conf"
RESOLVER_DIR="/etc/resolver"
RESOLVER_FILE="${RESOLVER_DIR}/orbit.test"

# ── Install dnsmasq ────────────────────────────────────────────────────────
info "Checking dnsmasq..."
if ! command -v dnsmasq &>/dev/null; then
  info "Installing dnsmasq via Homebrew..."
  brew install dnsmasq
  ok "dnsmasq installed"
else
  ok "dnsmasq already installed"
fi

# Ensure dnsmasq main config includes the drop-in directory.
DNSMASQ_MAIN_CONF="${BREW_PREFIX}/etc/dnsmasq.conf"
if [[ -f "${DNSMASQ_MAIN_CONF}" ]] && ! grep -q "conf-dir=${DNSMASQ_CONF_DIR}" "${DNSMASQ_MAIN_CONF}"; then
  echo "conf-dir=${DNSMASQ_CONF_DIR}" >> "${DNSMASQ_MAIN_CONF}"
fi

# ── Write orbit.conf ───────────────────────────────────────────────────────
info "Configuring dnsmasq for *.orbit.test..."
mkdir -p "${DNSMASQ_CONF_DIR}"

if [[ -f "${ORBIT_CONF}" ]] && grep -q "address=/.orbit.test/127.0.0.1" "${ORBIT_CONF}"; then
  ok "dnsmasq orbit.conf already configured"
else
  cat > "${ORBIT_CONF}" <<'EOF'
# Orbit local development DNS
# Resolves any *.orbit.test hostname to 127.0.0.1 on the host Mac.
# Inside k3d pods, CoreDNS is patched separately to point at the Docker bridge.
address=/.orbit.test/127.0.0.1
EOF
  ok "Written: ${ORBIT_CONF}"
fi

# ── Write /etc/resolver/orbit.test ───────────────────────────────────────
info "Configuring macOS resolver for .orbit.test (may prompt for sudo)..."

if [[ -f "${RESOLVER_FILE}" ]] && grep -q "nameserver 127.0.0.1" "${RESOLVER_FILE}"; then
  ok "Resolver already configured: ${RESOLVER_FILE}"
else
  sudo mkdir -p "${RESOLVER_DIR}"
  sudo tee "${RESOLVER_FILE}" > /dev/null <<'EOF'
# Direct all .orbit.test DNS queries to the local dnsmasq instance.
nameserver 127.0.0.1
EOF
  ok "Written: ${RESOLVER_FILE}"
fi

# ── Start / restart dnsmasq ────────────────────────────────────────────────
info "Starting dnsmasq service..."
# Use brew services so dnsmasq auto-starts on login.
# `restart` is idempotent — works whether dnsmasq is running or not.
sudo "${BREW_PREFIX}/bin/brew" services restart dnsmasq 2>/dev/null || "${BREW_PREFIX}/bin/brew" services restart dnsmasq
ok "dnsmasq running"

# ── Flush macOS DNS cache ──────────────────────────────────────────────────
info "Flushing macOS DNS cache..."
sudo dscacheutil -flushcache
sudo killall -HUP mDNSResponder
ok "DNS cache flushed"

# ── Verify ────────────────────────────────────────────────────────────────
info "Verifying DNS resolution..."
sleep 1  # give dnsmasq a moment to start

if dig +short dex.orbit.test @127.0.0.1 2>/dev/null | grep -q "127.0.0.1"; then
  ok "dex.orbit.test → 127.0.0.1  ✓"
else
  warn "dig verification inconclusive — dnsmasq may need a moment. Try:"
  warn "  dig +short dex.orbit.test @127.0.0.1"
fi

# ── Done ──────────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
echo -e "${GREEN}  DNS for *.orbit.test configured                ${RESET}"
echo -e "${GREEN}══════════════════════════════════════════════════${RESET}"
echo ""
echo "  Any *.orbit.test hostname now resolves to 127.0.0.1 on this Mac."
echo "  The k3d cluster is patched automatically during: make dev-cluster"
echo ""
echo "  Test:  dig +short dex.orbit.test"
echo "  Dex:   https://dex.orbit.test:5556/dex"
echo ""
