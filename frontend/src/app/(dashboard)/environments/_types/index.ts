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
  /** SandboxImage name, or "" for the kora base image. */
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

// ── Sandbox images ────────────────────────────────────────────────────────────

/** The SandboxImage the operator registers for the kora base image in every tenant. */
export const BASE_IMAGE_NAME = 'kora-base';

export const BASE_IMAGE_LABEL = 'kora base image';

/** The image an environment runs, as the UI names it. */
export function imageLabel(sandboxImage: string): string {
  return sandboxImage === '' || sandboxImage === BASE_IMAGE_NAME ? BASE_IMAGE_LABEL : sandboxImage;
}

/**
 * Why an environment's image cannot start a sandbox, or null when it can. A
 * null image list means the list did not load, and nothing is reported.
 */
export function imageProblem(sandboxImage: string, images: SandboxImage[] | null): string | null {
  if (images === null) return null;
  const name = sandboxImage || BASE_IMAGE_NAME;
  const image = images.find((i) => i.name === name);
  if (!image) return name === BASE_IMAGE_NAME ? null : 'Not registered in this tenant.';
  if (!image.valid) return image.problem || 'Not valid.';
  return null;
}

/** A compact size summary, e.g. "2 CPU · 4Gi memory · 10Gi disk". */
export function sizeLabel(size: EnvironmentSize): string {
  return [
    size.cpu && `${size.cpu} CPU`,
    size.memory && `${size.memory} memory`,
    size.disk && `${size.disk} disk`,
  ]
    .filter(Boolean)
    .join(' · ');
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
