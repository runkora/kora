import Link from 'next/link';
import { Alert, Button, Icon, Text } from '@orbit/terra';
import { EmptyState } from '@orbit/components';
import { getSession, isSessionValid } from '@orbit/server/session';
import { loadSkillDetail } from '../_services/marketplace-server';
import { MarketplaceSkillDetail } from '../_components/marketplace-skill-detail';
import { MarketplaceSkillLoadError } from '../_components/marketplace-skill-load-error';
import { buildOverviewUrl, parseBrowseParams, recordToSearchParams } from '../_utils/browse-params';

interface SkillDetailPageProps {
  params: Promise<{ slug: string[] }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}

export default async function SkillDetailPage({ params, searchParams }: SkillDetailPageProps) {
  const { slug: segments } = await params;
  // The Skill link carries the browse values of the overview. The header Back
  // button keeps them, and the "Skill not found" message uses them for its
  // link. parseBrowseParams validates each value, so the link always opens
  // the overview.
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

  const result = await loadSkillDetail(session.accessToken, segments.join('/'));

  if (!result.ok && result.status === 404) {
    // No Skill has this slug.
    return (
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
    );
  }

  if (!result.ok) {
    // The load failed. This is not a missing Skill. The notice has a retry.
    return <MarketplaceSkillLoadError />;
  }

  return <MarketplaceSkillDetail skill={result.skill} />;
}
