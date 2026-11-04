import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, resolve } from 'node:path';
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
    '../src/packages/hades/src/**/*.stories.@(ts|tsx)',
    '../src/components/**/*.stories.@(ts|tsx)',
    '../src/app/**/*.stories.@(ts|tsx)',
    './stories/**/*.stories.@(ts|tsx)',
  ],
  // Stories that render the default theme's logo need public/brand/.
  staticDirs: ['../public'],
  addons: ['@storybook/addon-docs', '@storybook/addon-a11y'],
  framework: {
    name: '@storybook/nextjs',
    options: {},
  },
  docs: { autodocs: 'tag' },
  core: { allowedHosts: readAllowedHosts() },
  // Hades and the app each resolve their own recharts, and charts render blank
  // when the container and the chart come from different copies. One copy only.
  webpackFinal: async (webpackConfig) => {
    webpackConfig.resolve = {
      ...webpackConfig.resolve,
      alias: {
        ...webpackConfig.resolve?.alias,
        recharts: dirname(createRequire(import.meta.url).resolve('recharts/package.json')),
      },
    };
    // webpack is not a direct dependency; take the copy Storybook's Next.js framework builds with.
    const webpack = createRequire(createRequire(import.meta.url).resolve('@storybook/nextjs'))(
      'webpack',
    ) as typeof import('webpack');
    // The policy grants server actions reach `server-only`, which throws in the browser bundle.
    // Imports are relative (`../_actions/grants`, `../../../policy/_actions/grants`), so match on
    // where the request resolves, not on its text: only the policy feature's module is replaced.
    const policyGrantsActions = resolve(
      process.cwd(),
      'src/app/(dashboard)/policy/_actions/grants',
    );
    webpackConfig.plugins = [
      ...(webpackConfig.plugins ?? []),
      new webpack.NormalModuleReplacementPlugin(/_actions\/grants$/, (resource) => {
        const target = resolve(resource.context, resource.request);
        if (target === policyGrantsActions) {
          resource.request = resolve(import.meta.dirname, 'mocks/policy-grants-actions.ts');
        }
      }),
    ];
    return webpackConfig;
  },
};

export default config;
