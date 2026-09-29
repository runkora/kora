import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { parseEnv } from 'node:util';
import type { StorybookConfig } from '@storybook/nextjs';

// Extra hostnames the dev server accepts, for reaching Storybook over a VPN.
// Read from the shell or from STORYBOOK_ALLOWED_HOSTS in the repo root .env.
// A leading dot matches the domain and every subdomain, for example ".ts.net".
function readAllowedHosts(): string[] {
  let value = process.env.STORYBOOK_ALLOWED_HOSTS;
  if (value === undefined) {
    try {
      value = parseEnv(
        readFileSync(resolve(process.cwd(), '../.env'), 'utf8'),
      ).STORYBOOK_ALLOWED_HOSTS;
    } catch {
      // No root .env, so there are no extra hosts.
    }
  }
  return (value ?? '')
    .split(',')
    .map((host) => host.trim())
    .filter(Boolean);
}

const config: StorybookConfig = {
  stories: [
    '../src/packages/terra/src/**/*.stories.@(ts|tsx)',
    '../src/components/**/*.stories.@(ts|tsx)',
    './stories/**/*.stories.@(ts|tsx)',
  ],
  addons: ['@storybook/addon-docs', '@storybook/addon-a11y'],
  framework: {
    name: '@storybook/nextjs',
    options: {},
  },
  docs: { autodocs: 'tag' },
  core: { allowedHosts: readAllowedHosts() },
};

export default config;
