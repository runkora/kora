import { describe, expect, it } from 'vitest';
import type { SandboxImage } from '@orbit/shared/api';
import { imageProblem, parseQuantity, sizeLabel, sizeProblems } from './environments';

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

describe('parseQuantity', () => {
  it('reads Kubernetes quantities', () => {
    expect(parseQuantity('2')).toBe(2);
    expect(parseQuantity('500m')).toBe(0.5);
    expect(parseQuantity('4Gi')).toBe(4 * 2 ** 30);
    expect(parseQuantity('1G')).toBe(1e9);
    expect(parseQuantity('1e3')).toBe(1000);
    expect(parseQuantity(' 1.5Ki ')).toBe(1536);
  });

  it('rejects anything else', () => {
    expect(parseQuantity('')).toBeNull();
    expect(parseQuantity('4GB')).toBeNull();
    expect(parseQuantity('-1')).toBeNull();
    expect(parseQuantity('two')).toBeNull();
  });
});

describe('sizeProblems', () => {
  const limits = {
    default: { cpu: '2', memory: '4Gi', disk: '10Gi' },
    max: { cpu: '8', memory: '16Gi', disk: '50Gi' },
  };

  it('accepts sizes within the maximum, in any unit', () => {
    expect(sizeProblems({ cpu: '8', memory: '16384Mi', disk: '' }, limits)).toEqual({});
  });

  it('reports each size above the maximum', () => {
    expect(sizeProblems({ cpu: '8500m', memory: '17Gi', disk: '50Gi' }, limits)).toEqual({
      cpu: 'Above the maximum of 8.',
      memory: 'Above the maximum of 16Gi.',
    });
  });

  it('reports values that are not quantities or not positive', () => {
    expect(sizeProblems({ cpu: 'lots', memory: '0', disk: '1Gi' }, limits)).toEqual({
      cpu: 'Not a valid quantity.',
      memory: 'Must be greater than zero.',
    });
  });

  it('leaves the maximum to the API when the limits did not load', () => {
    expect(sizeProblems({ cpu: '64', memory: '4Gi', disk: '10Gi' }, null)).toEqual({});
  });
});
