import { getSession, isSessionValid } from '@orbit/server/session';
import { getSkill } from '@orbit/server/marketplace-client';
import type { Skill } from '@orbit/lib/marketplace';
import { SkillShell } from './_components/skill-shell';

interface Props {
  params: Promise<{ marketplace: string; skill: string }>;
}

export default async function SkillPage({ params }: Props) {
  const { marketplace, skill } = await params;
  const session = await getSession();

  let data: Skill | null = null;
  let error: string | null = null;

  if (isSessionValid(session)) {
    try {
      data = await getSkill(session.accessToken, marketplace, skill);
    } catch {
      error = 'Skill not found';
    }
  }

  return <SkillShell marketplace={marketplace} skillName={skill} skill={data} error={error} />;
}
