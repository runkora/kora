// ── Display-layer types for the Environments feature ──────────────────────────
//
// EnvironmentRow is the shape consumed by the UI. The raw API shape
// (Environment) only appears at the service-layer boundary.

import type { Environment, Connector, EnvironmentSize, SandboxImage } from '@orbit/shared/api';

export type { Connector, EnvironmentSize, SandboxImage };

export interface EnvironmentRow {
  id: string;
  name: string;
  description: string;
  /** SandboxImage name. Environments stored before images existed have "" and run kora-base. */
  sandboxImage: string;
  size: EnvironmentSize;
  egressHosts: string[];
  connectors: Connector[];
  createdAt: string;
  updatedAt: string;
}

// ── Transformers ──────────────────────────────────────────────────────────────

export function toEnvironmentRow(e: Environment): EnvironmentRow {
  return {
    id: e.id,
    name: e.name,
    description: e.description,
    sandboxImage: e.sandbox_image ?? '',
    size: e.size ?? { cpu: '', memory: '', disk: '' },
    egressHosts: e.egress_hosts ?? [],
    connectors: e.connectors ?? [],
    createdAt: e.created_at,
    updatedAt: e.updated_at,
  };
}

// ── Curated host lists ────────────────────────────────────────────────────────

/**
 * Common public package registries an agent typically needs to fetch
 * dependencies (npm, PyPI, Go modules, Rust crates, Debian apt). Surfaced as a
 * one-click "quick add" in the egress-hosts editor.
 */
export const COMMON_PACKAGE_REGISTRIES: readonly string[] = [
  'registry.npmjs.org',
  'pypi.org',
  'files.pythonhosted.org',
  'proxy.golang.org',
  'sum.golang.org',
  'index.crates.io',
  'static.crates.io',
  'deb.debian.org',
];
