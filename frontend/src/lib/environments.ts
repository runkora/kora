/**
 * Display helpers for environments and their sandbox images, shared by the
 * environments and agents features.
 */

import type { EnvironmentSize, EnvironmentSizes, SandboxImage } from '@orbit/shared/api';

/** The SandboxImage the operator registers for the kora base image in every tenant. */
export const BASE_IMAGE_NAME = 'kora-base';

/**
 * Why an environment's image cannot start a sandbox, or null when it can. A
 * null image list means the list did not load, and nothing is reported.
 */
export function imageProblem(sandboxImage: string, images: SandboxImage[] | null): string | null {
  if (images === null) return null;
  const image = images.find((i) => i.name === sandboxImage);
  if (!image) return 'Not registered in this tenant.';
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

// Kubernetes quantity suffixes and the multiplier each stands for.
const QUANTITY_SUFFIXES: Record<string, number> = {
  '': 1,
  n: 1e-9,
  u: 1e-6,
  m: 1e-3,
  k: 1e3,
  M: 1e6,
  G: 1e9,
  T: 1e12,
  P: 1e15,
  E: 1e18,
  Ki: 2 ** 10,
  Mi: 2 ** 20,
  Gi: 2 ** 30,
  Ti: 2 ** 40,
  Pi: 2 ** 50,
  Ei: 2 ** 60,
};

const QUANTITY_RE =
  /^([0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:([eE][+-]?[0-9]+)|(Ki|Mi|Gi|Ti|Pi|Ei|[numkMGTPE]))?$/;

/** Parses a Kubernetes quantity such as "500m", "4Gi" or "1e3", or returns null when it is not one. */
export function parseQuantity(value: string): number | null {
  const match = QUANTITY_RE.exec(value.trim());
  if (!match) return null;
  const [, number, exponent, suffix] = match;
  return (
    Number(number) * (exponent ? 10 ** Number(exponent.slice(1)) : QUANTITY_SUFFIXES[suffix ?? ''])
  );
}

/**
 * The problem with each size field, keyed by dimension. An empty field takes
 * the platform default and is fine. The API checks the same rules on save.
 */
export function sizeProblems(
  size: EnvironmentSize,
  limits: EnvironmentSizes | null,
): Partial<Record<keyof EnvironmentSize, string>> {
  const problems: Partial<Record<keyof EnvironmentSize, string>> = {};
  for (const key of ['cpu', 'memory', 'disk'] as const) {
    const value = size[key].trim();
    if (value === '') continue;
    const quantity = parseQuantity(value);
    if (quantity === null) {
      problems[key] = 'Not a valid quantity.';
    } else if (quantity <= 0) {
      problems[key] = 'Must be greater than zero.';
    } else if (limits) {
      const max = parseQuantity(limits.max[key]);
      if (max !== null && quantity > max)
        problems[key] = `Above the maximum of ${limits.max[key]}.`;
    }
  }
  return problems;
}
