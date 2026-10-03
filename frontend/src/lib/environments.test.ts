import { describe, expect, it } from 'vitest';
import type { SandboxImage } from '@orbit/shared/api';
import { imageLabel, imageProblem, sizeLabel } from './environments';

const image = (name: string, valid: boolean, problem?: string): SandboxImage => ({
  name,
  description: '',
  image: `registry.example/${name}:1`,
  valid,
  problem,
  warm: false,
});

describe('imageLabel', () => {
  it('names the empty image and kora-base as the kora base image', () => {
    expect(imageLabel('')).toBe('kora base image');
    expect(imageLabel('kora-base')).toBe('kora base image');
    expect(imageLabel('acme-go')).toBe('acme-go');
  });
});

describe('imageProblem', () => {
  const images = [image('acme-go', true), image('acme-ops', false, 'runs as root')];

  it('reports nothing for a valid image or the base image', () => {
    expect(imageProblem('acme-go', images)).toBeNull();
    expect(imageProblem('', images)).toBeNull();
  });

  it('reports the operator reason for an invalid image', () => {
    expect(imageProblem('acme-ops', images)).toBe('runs as root');
  });

  it('reports an image that is not registered', () => {
    expect(imageProblem('gone', images)).toBe('Not registered in this tenant.');
  });

  it('reports an invalid base image', () => {
    expect(imageProblem('', [image('kora-base', false, 'no base label')])).toBe('no base label');
  });

  it('reports nothing when the image list did not load', () => {
    expect(imageProblem('gone', null)).toBeNull();
  });
});

describe('sizeLabel', () => {
  it('lists each set dimension', () => {
    expect(sizeLabel({ cpu: '2', memory: '4Gi', disk: '10Gi' })).toBe(
      '2 CPU · 4Gi memory · 10Gi disk',
    );
    expect(sizeLabel({ cpu: '', memory: '', disk: '' })).toBe('');
  });
});
