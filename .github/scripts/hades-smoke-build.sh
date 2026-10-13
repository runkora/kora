#!/usr/bin/env bash
# Smoke build for a packed Hades tarball: the proof that the package builds
# outside the kora frontend. Scaffolds a minimal Vite + React + Tailwind v4
# application in a temporary directory, installs the tarball and the peer
# dependencies it declares, renders a page that imports tokens.css, the Button
# component, HADES_VERSION, CONTRACT_VERSION and the theme module's validator from
# the `@clear-route/hades/theme` subpath, type-checks it and runs the production
# build.
#
# Usage: .github/scripts/hades-smoke-build.sh <path/to/clear-route-hades-X.Y.Z.tgz>
#
# The release workflow runs this on every hades/v* tag (see the Hades delivery
# ADR); `make hades-smoke` packs and runs it locally. It needs network access to
# the public npm registry for Vite, React and Tailwind. The temporary directory
# is removed on success and kept, with its path printed, on failure.
set -euo pipefail

if [ $# -ne 1 ]; then
  echo "usage: $0 <tarball>" >&2
  exit 2
fi

tarball=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
if [ ! -f "$tarball" ]; then
  echo "error: $tarball is not a file" >&2
  exit 2
fi

app=$(mktemp -d "${TMPDIR:-/tmp}/hades-smoke.XXXXXX")
cleanup() {
  status=$?
  if [ "$status" -eq 0 ]; then
    rm -rf "$app"
  else
    echo "smoke build failed (exit $status); the app is kept at $app" >&2
  fi
}
trap cleanup EXIT

# The package's own manifest decides what a consumer must install: every peer
# dependency at its declared range, nothing hard-coded here.
pkg=$(tar -xzOf "$tarball" package/package.json)
version=$(printf '%s' "$pkg" | node -pe 'JSON.parse(require("fs").readFileSync(0, "utf8")).version')
peers=()
while IFS= read -r peer; do
  peers+=("$peer")
done < <(printf '%s' "$pkg" | node -e '
  const pkg = JSON.parse(require("fs").readFileSync(0, "utf8"));
  for (const [name, range] of Object.entries(pkg.peerDependencies ?? {})) console.log(`${name}@${range}`);
')

echo "smoke build: @clear-route/hades@$version in $app"

mkdir -p "$app/src"
cd "$app"

cat > package.json <<'EOF'
{
  "name": "hades-smoke",
  "private": true,
  "type": "module",
  "scripts": {
    "build": "tsc --noEmit && vite build"
  }
}
EOF

cat > tsconfig.json <<'EOF'
{
  "compilerOptions": {
    "target": "ES2022",
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "jsx": "react-jsx",
    "strict": true,
    "skipLibCheck": true,
    "isolatedModules": true,
    "noEmit": true
  },
  "include": ["src"]
}
EOF

cat > vite.config.ts <<'EOF'
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  plugins: [react(), tailwindcss()],
});
EOF

cat > index.html <<'EOF'
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <title>Hades smoke build</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
EOF

# tokens.css is the single Tailwind entry: it imports tailwindcss itself, so the
# application never adds a second `@import 'tailwindcss'`. The @source makes
# Tailwind scan the installed package for the utilities its components use.
cat > src/index.css <<'EOF'
@import '@clear-route/hades/src/tokens/tokens.css';
@source '../node_modules/@clear-route/hades/src';

:root {
  --hades-raw-font-sans: ui-sans-serif, system-ui, sans-serif;
  --hades-raw-font-mono: ui-monospace, monospace;
}
EOF

cat > src/main.tsx <<'EOF'
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { Button, CONTRACT_VERSION, HADES_VERSION } from '@clear-route/hades';
import { validateTheme } from '@clear-route/hades/theme';
import './index.css';

function App() {
  return (
    <main>
      <Button variant="secondary">
        Hades {HADES_VERSION}, contract {CONTRACT_VERSION} ({typeof validateTheme})
      </Button>
    </main>
  );
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
EOF

echo "installing the tarball, ${#peers[@]} peer dependencies and the Vite toolchain"
npm install --no-audit --no-fund --loglevel=error \
  "$tarball" \
  "${peers[@]}" \
  vite@^8 @vitejs/plugin-react@^6 @tailwindcss/vite@^4 \
  typescript@^5 @types/react@^19 @types/react-dom@^19

installed=$(node -pe 'require("./node_modules/@clear-route/hades/package.json").version')
if [ "$installed" != "$version" ]; then
  echo "error: installed @clear-route/hades@$installed, expected $version" >&2
  exit 1
fi

echo "type-checking and building"
npm run build --silent

# The build only proves something if the page actually carried Hades through
# it: the version string in the bundle and a token from tokens.css in the CSS.
js=$(find dist/assets -name '*.js' | head -1)
css=$(find dist/assets -name '*.css' | head -1)
if [ -z "$js" ] || [ -z "$css" ]; then
  echo "error: the build produced no JS or CSS asset" >&2
  exit 1
fi
if ! grep -qF "$version" "$js"; then
  echo "error: HADES_VERSION $version is not in the built bundle" >&2
  exit 1
fi
contract=$(node -pe 'require("./node_modules/@clear-route/hades/manifest.json").contractVersion')
if ! grep -qF "\"$contract\"" "$js"; then
  echo "error: CONTRACT_VERSION $contract is not in the built bundle" >&2
  exit 1
fi
if ! grep -q -- '--hades-raw-font-sans' "$css"; then
  echo "error: the built CSS carries no Hades token; tokens.css was not compiled" >&2
  exit 1
fi

echo "smoke build passed: @clear-route/hades@$version builds outside kora"
