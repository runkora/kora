#!/usr/bin/env bash
# Smoke build: proves a packed Hades tarball builds in a minimal Vite app outside kora. Needs the public npm registry.
# Usage: .github/scripts/hades-smoke-build.sh <path/to/runkora-hades-X.Y.Z.tgz>
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

# Install each peer dependency at the range the package declares.
pkg=$(tar -xzOf "$tarball" package/package.json)
version=$(printf '%s' "$pkg" | node -pe 'JSON.parse(require("fs").readFileSync(0, "utf8")).version')
peers=()
while IFS= read -r peer; do
  peers+=("$peer")
done < <(printf '%s' "$pkg" | node -e '
  const pkg = JSON.parse(require("fs").readFileSync(0, "utf8"));
  for (const [name, range] of Object.entries(pkg.peerDependencies ?? {})) console.log(`${name}@${range}`);
')

echo "smoke build: @runkora/hades@$version in $app"

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

# tokens.css is the single Tailwind entry. The @source makes Tailwind scan the installed package.
cat > src/index.css <<'EOF'
@import '@runkora/hades/src/tokens/tokens.css';
@source '../node_modules/@runkora/hades/src';

:root {
  --hades-raw-font-sans: ui-sans-serif, system-ui, sans-serif;
  --hades-raw-font-mono: ui-monospace, monospace;
}
EOF

cat > src/main.tsx <<'EOF'
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { Button } from '@runkora/hades';
import { validateTheme } from '@runkora/hades/theme';
import './index.css';

function App() {
  return (
    <main>
      <Button variant="secondary">Hades smoke build ({typeof validateTheme})</Button>
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

installed=$(node -pe 'require("./node_modules/@runkora/hades/package.json").version')
if [ "$installed" != "$version" ]; then
  echo "error: installed @runkora/hades@$installed, expected $version" >&2
  exit 1
fi

echo "type-checking and building"
npm run build --silent

# Proves the page carried Hades through: its text in the bundle and a tokens.css token in the CSS.
js=$(find dist/assets -name '*.js' | head -1)
css=$(find dist/assets -name '*.css' | head -1)
if [ -z "$js" ] || [ -z "$css" ]; then
  echo "error: the build produced no JS or CSS asset" >&2
  exit 1
fi
if ! grep -qF "Hades smoke build" "$js"; then
  echo "error: the page rendered through Hades is not in the built bundle" >&2
  exit 1
fi
if ! grep -q -- '--hades-raw-accent-500' "$css"; then
  echo "error: the built CSS carries no Hades token; tokens.css was not compiled" >&2
  exit 1
fi

echo "smoke build passed: @runkora/hades@$version builds outside kora"
