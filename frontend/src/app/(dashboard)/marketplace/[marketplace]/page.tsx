import { getSession, isSessionValid } from '@orbit/server/session';
import { listSkills } from '@orbit/server/marketplace-client';
import type { SkillSummary } from '@orbit/lib/marketplace';
import { SkillsShell } from './_components/skills-shell';

interface Props {
  params: Promise<{ marketplace: string }>;
  searchParams: Promise<{ search?: string }>;
}

export default async function SkillsPage({ params, searchParams }: Props) {
  const { marketplace } = await params;
  const { search } = await searchParams;
  const session = await getSession();

  let skills: SkillSummary[] = [];
  let error: string | null = null;

  if (isSessionValid(session)) {
    try {
      skills = await listSkills(session.accessToken, marketplace, search);
    } catch {
      error = 'Failed to load skills';
    }
  }

  return (
    <SkillsShell marketplace={marketplace} skills={skills} search={search ?? ''} error={error} />
  );
}
