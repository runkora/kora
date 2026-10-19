import { getSession, isSessionValid } from '@runkora/server/session';
import { fetchRoutes } from '../_services/policy-server';
import { RoutesTab } from '../_components/routes-tab';

export default async function RoutesPage() {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return <RoutesTab initialRegistrations={[]} initialError="Not authenticated" />;
  }

  const result = await fetchRoutes(session.accessToken);
  const registrations = result.ok ? (result.data.registrations ?? []) : [];
  const error = !result.ok ? result.message : null;

  return <RoutesTab initialRegistrations={registrations} initialError={error} />;
}
