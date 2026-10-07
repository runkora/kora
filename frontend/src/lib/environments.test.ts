import { describe, expect, it } from 'vitest';
import type { SandboxImage } from '@orbit/shared/api';
import { imageProblem, sizeLabel, slugProblem } from './environments';

const image = (name: string, valid: boolean, problem?: string): SandboxImage => ({
  name,
  description: '',
  image: `registry.example/${name}:1`,
  valid,
  problem,
  warm: false,
});

describe('imageProblem', () => {
  const images = [image('acme-go', true), image('acme-ops', false, 'runs as root')];

  it('reports nothing for a valid image', () => {
    expect(imageProblem('acme-go', images)).toBeNull();
  });

  it('reports the operator reason for an invalid image', () => {
    expect(imageProblem('acme-ops', images)).toBe('runs as root');
  });

  it('reports an image that is not registered', () => {
    expect(imageProblem('gone', images)).toBe('Not registered in this tenant.');
  });

  it('treats kora-base like any other image', () => {
    expect(imageProblem('kora-base', images)).toBe('Not registered in this tenant.');
    expect(imageProblem('kora-base', [image('kora-base', false, 'no base label')])).toBe(
      'no base label',
    );
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

describe('slugProblem', () => {
  it('accepts a DNS-1123 label', () => {
    expect(slugProblem('go-services')).toBeNull();
  });

  it.each([
    ['', 'Enter a slug.'],
    ['a'.repeat(64), 'Use at most 63 characters.'],
    ['-go', 'Use lowercase letters, digits and hyphens.'],
    ['Go', 'Use lowercase letters, digits and hyphens.'],
  ])('rejects %j', (slug, problem) => {
    expect(slugProblem(slug)).toBe(problem);
  });
});
