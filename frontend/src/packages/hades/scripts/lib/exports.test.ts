import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { describe, expect, it } from 'vitest';

import { readExportSurface, type FileReader } from './exports.ts';

function reader(files: Record<string, string>): FileReader {
  return (rel) => files[rel] ?? null;
}

describe('readExportSurface', () => {
  it('follows re-exports to the declaring file and lists own members', () => {
    const read = reader({
      'src/index.ts': [
        "export { cn } from './utils';",
        "export { Button, buttonVariants } from './components/button';",
        "export type { ButtonProps, ButtonSize } from './components/button';",
        "export * from './components/card';",
      ].join('\n'),
      'src/utils.ts': 'export function cn() {}',
      'src/components/button/index.ts': [
        "export { Button, buttonVariants } from './button';",
        "export type { ButtonProps, ButtonSize } from './button';",
      ].join('\n'),
      'src/components/button/button.tsx': [
        "export type ButtonSize = 'sm' | 'md';",
        'export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {',
        '  asChild?: boolean;',
        '  dim?: boolean;',
        '  size?: ButtonSize;',
        '}',
        'const Button = () => null;',
        'const buttonVariants = () => "";',
        'export { Button, buttonVariants };',
      ].join('\n'),
      'src/components/card/index.ts':
        "export { Card } from './card';\nexport type { CardProps } from './card';",
      'src/components/card/card.tsx': [
        "export type CardProps = React.HTMLAttributes<HTMLDivElement> & { padded?: boolean; 'data-x'?: string };",
        'export const Card = () => null;',
      ].join('\n'),
    });
    const surface = readExportSurface(read);
    expect(surface.exports).toEqual([
      'Button',
      'ButtonProps',
      'ButtonSize',
      'Card',
      'CardProps',
      'buttonVariants',
      'cn',
    ]);
    expect(surface.props).toEqual({
      ButtonProps: ['asChild', 'dim', 'size'],
      CardProps: ['data-x', 'padded'],
    });
  });

  it('records which members are required', () => {
    const read = reader({
      'src/index.ts': "export type { FieldProps, PanelProps } from './field';",
      'src/field.tsx': [
        'export interface FieldProps {',
        '  label: string;',
        '  hint?: string;',
        '  onChange(value: string): void;',
        '}',
        'export type PanelProps = { open: boolean } & { dense?: boolean };',
      ].join('\n'),
    });
    const surface = readExportSurface(read);
    expect(surface.required).toEqual({
      FieldProps: ['label', 'onChange'],
      PanelProps: ['open'],
    });
    expect(surface.props.PanelProps).toEqual(['dense', 'open']);
  });

  it('omits a type it cannot resolve to declared members', () => {
    const read = reader({
      'src/index.ts': "export type { ShowProps } from './show';",
      'src/show.tsx': 'export type ShowProps = React.ComponentProps<typeof Show>;',
    });
    expect(readExportSurface(read)).toEqual({ exports: ['ShowProps'], props: {}, required: {} });
  });

  it('reads the real package entry point', () => {
    const packageDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
    const read: FileReader = (rel) => {
      try {
        return fs.readFileSync(path.join(packageDir, rel), 'utf8');
      } catch {
        return null;
      }
    };
    const surface = readExportSurface(read);
    expect(surface.exports).toContain('Button');
    expect(surface.exports).toContain('HADES_VERSION');
    expect(surface.exports).toContain('CONTRACT_VERSION');
    expect(surface.props.ButtonProps).toContain('asChild');
  });
});
