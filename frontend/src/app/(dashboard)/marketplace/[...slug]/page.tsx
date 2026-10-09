import Link from 'next/link';
import { Alert, Button, EmptyState, Icon, Stack, Text } from '@orbit/terra';
import { getSession, isSessionValid } from '@orbit/server/session';
import { loadSkillDetail } from '../_services/marketplace-server';
import { MarketplaceSkillDetail } from '../_components/marketplace-skill-detail';
import { MarketplaceSkillLoadError } from '../_components/marketplace-skill-load-error';
import {
  buildOverviewUrl,
  parseBrowseParams,
  parseSkillSlug,
  recordToSearchParams,
} from '../_utils/browse-params';

interface SkillDetailPageProps {
  params: Promise<{ slug: string[] }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}

export default async function SkillDetailPage({ params, searchParams }: SkillDetailPageProps) {
  const { slug: segments } = await params;
  // The Skill link carries the browse values of the overview. The back link of
  // this page and the "Skill not found" message use them, so that they return
  // to the same results (PRD FR-17). parseBrowseParams validates each value,
  // so the link always opens the overview.
  const backHref = buildOverviewUrl(parseBrowseParams(recordToSearchParams(await searchParams)));
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <Alert variant="error">
        <Icon name="lock" color="status-error" />
        <Text size="caption" color="status-error">
          Session expired. Please sign in again to view this Skill.
        </Text>
      </Alert>
    );
  }

  const slug = parseSkillSlug(segments);
  const result = slug === null ? null : await loadSkillDetail(session.accessToken, slug);

  if (!result || (!result.ok && result.status === 404)) {
    // No Skill has this slug.
    return (
      <Stack gap="comfortable">
        <BackToSkillsLink href={backHref} />
        <EmptyState
          icon="box"
          title="Skill not found"
          description="No Skill in this Marketplace matches this address."
          action={
            <Button asChild>
              <Link href={backHref}>Back to Skills</Link>
            </Button>
          }
        />
      </Stack>
    );
  }

  if (!result.ok) {
    // The load failed. This is not a missing Skill. The notice has a retry.
    return (
      <Stack gap="comfortable">
        <BackToSkillsLink href={backHref} />
        <MarketplaceSkillLoadError />
      </Stack>
    );
  }

  return (
    <Stack gap="comfortable">
      <BackToSkillsLink href={backHref} />
      <MarketplaceSkillDetail skill={result.skill} />
    </Stack>
  );
}

/** The link back to the overview results that the Skill URL carries. */
function BackToSkillsLink({ href }: { href: string }) {
  return (
    <Stack direction="horizontal">
      <Button variant="ghost" size="sm" asChild>
        <Link href={href}>
          <Icon name="arrow-left" size="xs" />
          Skills
        </Link>
      </Button>
    </Stack>
  );
}
