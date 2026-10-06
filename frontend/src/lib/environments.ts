/**
 * Display helpers for environments and their sandbox images, shared by the
 * environments and agents features.
 */

import type { EnvironmentSize, SandboxImage } from '@orbit/shared/api';

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
