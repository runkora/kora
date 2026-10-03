/**
 * Display helpers for environments and their sandbox images, shared by the
 * environments and agents features.
 */

import type { EnvironmentSize, SandboxImage } from '@orbit/shared/api';

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
