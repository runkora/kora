/**
 * server-only shim for test environments.
 *
 * The `server-only` package throws at import time when bundled into a
 * non-server context. In Vitest (Node, not a Next.js server runtime) the
 * guard fires even though we are effectively server-side. This empty module
 * replaces it via a resolve.alias in both Vitest configs so that server
 * modules (auth-config, config, session) can be imported in tests without
 * crashing.
 */
