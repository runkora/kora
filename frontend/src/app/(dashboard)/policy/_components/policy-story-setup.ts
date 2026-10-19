import { dashboardPage, sessionRoutes } from '@runkora/storybook/dashboard-frame';
import type { MockRoutes } from '@runkora/storybook/mock-api';
import PolicyLayout from '../layout';
import { POLICY_WRITE_SCOPES } from './policy-mock-data';

/** Wraps a story in the dashboard chrome and the policy layout, with write scopes so action buttons show. */
export const withPolicy = (routes: MockRoutes = {}) => [
  dashboardPage({ ...sessionRoutes(POLICY_WRITE_SCOPES), ...routes }, PolicyLayout),
];

/** Storybook parameters shared by every policy page story. */
export const policyParameters = (pathname: string) => ({
  layout: 'fullscreen' as const,
  nextjs: { appDirectory: true, navigation: { pathname } },
});
