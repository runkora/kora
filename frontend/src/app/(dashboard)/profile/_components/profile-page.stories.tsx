import type { Meta, StoryObj } from '@storybook/react';
import { Heading, Stack, Text } from '@orbit/terra';
import type { ListPATsResult } from '@orbit/server/services/identity-server';
import { dashboardPage } from '@orbit/storybook/dashboard-frame';
import { ApiKeysCard } from './api-keys-card';
import { RolesPermissionsCard } from './roles-permissions-card';
import { UserIdentityCard } from './user-identity-card';
import {
  EXPLAIN,
  EXPLAIN_EMPTY,
  EXPLAIN_FAILED,
  PROFILE,
  PROFILE_UNAVAILABLE,
  SESSION,
  TOKENS_EMPTY,
  TOKENS_FAILED,
  TOKENS_OK,
} from './profile-mock-data';

interface ProfileViewProps {
  profile: typeof PROFILE | typeof PROFILE_UNAVAILABLE;
  explain: { ok: true; data: typeof EXPLAIN } | typeof EXPLAIN_FAILED;
  tokens: ListPATsResult;
}

/** The page body of `profile/page.tsx`, which is an async server component and cannot render in a story. */
function ProfileView({ profile, explain, tokens }: ProfileViewProps) {
  return (
    <Stack gap="comfortable">
      <Stack gap="none">
        <Heading variant="title">Profile</Heading>
        <Text size="supporting" color="muted">
          Your identity, permissions, and API keys.
        </Text>
      </Stack>
      <UserIdentityCard session={SESSION} profile={profile} />
      <RolesPermissionsCard
        explain={explain}
        cachedScopes={explain.ok ? explain.data.effective_scopes : []}
        principalDisplayName={profile.ok ? profile.data.display_name : SESSION.user.name}
      />
      <ApiKeysCard tokens={tokens} />
    </Stack>
  );
}

/** The profile page as an operator sees it, with mock identity, permissions and API keys. */
const meta: Meta<typeof ProfileView> = {
  title: 'Pages/Profile',
  component: ProfileView,
  decorators: [dashboardPage()],
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/profile' } },
  },
};

export default meta;
type Story = StoryObj<typeof ProfileView>;

export const Default: Story = {
  args: { profile: PROFILE, explain: { ok: true, data: EXPLAIN }, tokens: TOKENS_OK },
};

/** A new user with no roles, grants or API keys. */
export const Empty: Story = {
  args: {
    profile: PROFILE,
    explain: { ok: true, data: EXPLAIN_EMPTY },
    tokens: TOKENS_EMPTY,
  },
};

/** Every backing service failed: the cards fall back to session data and error messages. */
export const FailedLoad: Story = {
  args: { profile: PROFILE_UNAVAILABLE, explain: EXPLAIN_FAILED, tokens: TOKENS_FAILED },
};
