#!/usr/bin/env bash
# =============================================================================
# setup-signing.sh — Configure this repository to sign commits and tags.
#
# Commits to main must be signed. The script signs with an SSH key, which every
# contributor already has for GitHub, and registers that key with GitHub as a
# signing key through the gh CLI so commits show as verified.
#
# A signing setup that already exists (an SSH or GPG key with commit.gpgsign on)
# is left alone. The settings are written to this repository's config only, so
# other repositories on the machine keep their own.
#
# Run through `make hooks`. Safe to re-run.
# =============================================================================
set -euo pipefail

GREEN="\033[32m"
YELLOW="\033[33m"
RESET="\033[0m"

ok() { printf "${GREEN}✓ %s${RESET}\n" "$1"; }
warn() { printf "${YELLOW}! %s${RESET}\n" "$1"; }

if [ "$(git config --bool commit.gpgsign || true)" = "true" ] && [ -n "$(git config user.signingkey || true)" ]; then
  ok "Commit signing already configured"
  signing_key="$(git config user.signingkey)"
  format="$(git config gpg.format || echo openpgp)"
else
  signing_key=""
  for candidate in "$HOME/.ssh/id_ed25519.pub" "$HOME/.ssh/id_ecdsa.pub" "$HOME/.ssh/id_rsa.pub"; do
    if [ -f "$candidate" ]; then
      signing_key="$candidate"
      break
    fi
  done
  if [ -z "$signing_key" ]; then
    warn "No SSH public key found in ~/.ssh. Create one with: ssh-keygen -t ed25519, then run: make hooks"
    exit 0
  fi
  git config --local gpg.format ssh
  git config --local user.signingkey "$signing_key"
  git config --local commit.gpgsign true
  git config --local tag.gpgsign true
  format="ssh"
  ok "Commits and tags in this repository are signed with $signing_key"
fi

# GitHub verifies an SSH signature only against keys registered as signing keys,
# which is separate from registering the same key for authentication.
if [ "$format" != "ssh" ] || [ ! -f "$signing_key" ]; then
  exit 0
fi
if ! command -v gh >/dev/null 2>&1 || ! gh auth status >/dev/null 2>&1; then
  warn "Add $signing_key on GitHub under Settings, SSH and GPG keys, as a Signing key"
  exit 0
fi

key_body="$(awk '{print $2}' "$signing_key")"
if gh ssh-key list 2>/dev/null | grep -F "$key_body" | grep -qi "signing"; then
  ok "Signing key registered with GitHub"
elif gh ssh-key add "$signing_key" --type signing --title "kora signing key" >/dev/null 2>&1; then
  ok "Signing key registered with GitHub"
else
  warn "Could not register the signing key with GitHub. Run: gh auth refresh -s admin:ssh_signing_key, then: make hooks"
fi
