// RSC: loads one skill by its folder path, with its Marketplace for the Synced row.

import Link from 'next/link';
import { Button, EmptyState, Icon, Stack } from '@runkora/hades';
import { APP_ROUTES } from '@runkora/lib/routing';
import { loadErrorMessage } from '@runkora/lib/load-error';
import { getSession, isSessionValid } from '@runkora/server/session';
import { loadMarketplaces, loadSkill } from '../../_services/skills-server';
import { browseQuery, decodeParam, toBrowse } from '../../_types';
import { SkillDetail } from './_components/skill-detail';
import { LoadErrorAlert } from '../../_components/load-error-alert';

interface SkillPageProps {
  params: Promise<{ marketplace: string; path: string[] }>;
  /** The overview's browse values, carried so the Skills link returns to the same results. */
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}

export default async function SkillPage({ params, searchParams }: SkillPageProps) {
  const [route, query] = await Promise.all([params, searchParams]);
  const marketplace = decodeParam(route.marketplace);
  const path = route.path.map(decodeParam).join('/');
  const session = await getSession();
  const [result, marketplaces] = isSessionValid(session)
    ? await Promise.all([
        loadSkill(session.accessToken, marketplace, path),
        loadMarketplaces(session.accessToken),
      ])
    : [{ ok: false as const, status: 401, message: loadErrorMessage(401, 'this skill') }, null];
  const overview = `${APP_ROUTES.skills.root}${browseQuery(toBrowse(query))}`;

  return (
    <Stack gap="comfortable">
      <Stack direction="horizontal">
        <Button variant="ghost" size="sm" asChild>
          <Link href={overview}>
            <Icon name="arrow-left" size="xs" />
            Skills
          </Link>
        </Button>
      </Stack>

      {result.ok ? (
        <SkillDetail
          skill={result.data}
          marketplace={
            marketplaces?.ok ? marketplaces.data.find((m) => m.slug === marketplace) : undefined
          }
        />
      ) : result.status === 404 ? (
        <EmptyState
          icon="box"
          title="Skill not found"
          description="No skill in this Marketplace matches this address."
          action={
            <Button asChild>
              <Link href={overview}>Back to Skills</Link>
            </Button>
          }
        />
      ) : (
        <LoadErrorAlert error={{ status: result.status, message: result.message }} />
      )}
    </Stack>
  );
}
