// RSC: loads one skill by its folder path.

import Link from 'next/link';
import { Button, EmptyState, Icon, Stack } from '@runkora/hades';
import { APP_ROUTES } from '@runkora/lib/routing';
import { loadErrorMessage } from '@runkora/lib/load-error';
import { getSession, isSessionValid } from '@runkora/server/session';
import { loadSkill } from '../../_services/skills-server';
import { SkillDetail } from './_components/skill-detail';
import { SkillLoadError } from './_components/skill-load-error';

interface SkillPageProps {
  params: Promise<{ marketplace: string; path: string[] }>;
}

export default async function SkillPage({ params }: SkillPageProps) {
  const { marketplace, path } = await params;
  const session = await getSession();
  const result = isSessionValid(session)
    ? await loadSkill(session.accessToken, marketplace, path.join('/'))
    : { ok: false as const, status: 401, message: loadErrorMessage(401, 'this skill') };
  const overview = APP_ROUTES.skills.marketplace(marketplace);

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
        <SkillDetail skill={result.data} />
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
        <SkillLoadError status={result.status} message={result.message} />
      )}
    </Stack>
  );
}
