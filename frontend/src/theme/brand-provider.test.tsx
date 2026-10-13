import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { BrandLogo, Text } from '@clear-route/hades';
import { BrandProvider, useBrand } from './brand-provider';
import { defaultTheme, type IdentityTheme } from './default';

function Probe() {
  const brand = useBrand();
  return (
    <>
      <Text>name: {brand.productName}</Text>
      <BrandLogo dark={brand.logo.dark} light={brand.logo.light} title={brand.productName} />
      <Text>favicon: {brand.favicon?.dark ?? 'none'}</Text>
    </>
  );
}

const ACME: IdentityTheme = {
  productName: 'Acme Agents',
  logo: { dark: '/theme/h/assets/logo-dark.svg', light: '/theme/h/assets/logo-light.svg' },
  favicon: { dark: '/theme/h/assets/favicon.svg' },
};

describe('BrandProvider and useBrand', () => {
  it('gives the kora default to a tree rendered without the provider', () => {
    render(<Probe />);
    expect(screen.getByText(`name: ${defaultTheme.productName}`)).toBeInTheDocument();
    expect(screen.getByRole('img', { name: defaultTheme.productName })).toBeInTheDocument();
    expect(screen.getByText('favicon: none')).toBeInTheDocument();
  });

  it('gives the provided brand to the tree beneath it', () => {
    render(
      <BrandProvider brand={ACME}>
        <Probe />
      </BrandProvider>,
    );
    expect(screen.getByText('name: Acme Agents')).toBeInTheDocument();
    const logo = screen.getByRole('img', { name: 'Acme Agents' });
    expect(logo.querySelector('img[data-mode="dark"]')).toHaveAttribute('src', ACME.logo.dark);
    expect(logo.querySelector('img[data-mode="light"]')).toHaveAttribute('src', ACME.logo.light);
    expect(screen.getByText(`favicon: ${ACME.favicon!.dark}`)).toBeInTheDocument();
  });
});
