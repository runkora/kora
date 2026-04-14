#!/bin/sh
# Substitutes ${GLAUTH_DEV_PASSWORD_HASH} from the environment into the
# committed config template, then starts glauth. Only shell builtins are
# used because the glauth image is minimal (no sed, envsubst, etc.).
set -e

while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in
    *'${GLAUTH_DEV_PASSWORD_HASH}'*)
      p="${line%%\$\{GLAUTH_DEV_PASSWORD_HASH\}*}"
      s="${line#*\$\{GLAUTH_DEV_PASSWORD_HASH\}}"
      echo "${p}${GLAUTH_DEV_PASSWORD_HASH}${s}" ;;
    *) echo "$line" ;;
  esac
done < /etc/glauth/config.cfg.in > /tmp/config.cfg

exec /app/glauth -c /tmp/config.cfg
