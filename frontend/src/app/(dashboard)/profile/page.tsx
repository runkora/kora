import { Heading, Stack, Text } from '@runkora/hades';
import { getSession, isSessionValid } from '@runkora/server/session';
import { listPATsFromIdentity } from '@runkora/server/services/identity-server';
import { fetchScopesExplain } from '@runkora/server/services/policy-gateway-server';
import { fetchUserProfile } from './_services/profile-server';
import { UserIdentityCard } from './_components/user-identity-card';
import { RolesPermissionsCard } from './_components/roles-permissions-card';
import { ApiKeysCard } from './_components/api-keys-card';

export default async function ProfilePage() {
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <Stack gap="comfortable">
        <Stack gap="none">
          <Heading variant="title">Profile</Heading>
          <Text size="supporting" color="muted">
            Your identity, permissions, and API keys.
          </Text>
        </Stack>
        <Text color="status-error">Session expired. Please log in again.</Text>
      </Stack>
    );
  }

  const [profileResult, explainResult, tokensResult] = await Promise.all([
    fetchUserProfile(session.accessToken, session.user.sub),
    fetchScopesExplain(session.accessToken),
    listPATsFromIdentity(session.accessToken),
  ]);

  return (
    <Stack gap="comfortable">
      <Stack gap="none">
        <Heading variant="title">Profile</Heading>
        <Text size="supporting" color="muted">
          Your identity, permissions, and API keys.
        </Text>
      </Stack>
      <UserIdentityCard session={session} profile={profileResult} />
      <RolesPermissionsCard
        explain={explainResult}
        cachedScopes={explainResult.ok ? explainResult.data.effective_scopes : []}
        principalDisplayName={
          profileResult.ok ? profileResult.data.display_name : session.user.name
        }
      />
      <ApiKeysCard tokens={tokensResult} />
    </Stack>
  );
}
