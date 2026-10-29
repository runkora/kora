// RSC: loads the requested Marketplace and Skill from the detail path.

import Link from 'next/link';
import { Alert, Button, EmptyState, Icon, Stack, Text } from '@orbit/hades';
import { APP_ROUTES } from '@orbit/lib/routing';
import { getSession, isSessionValid } from '@orbit/server/session';
import { loadSkillPage } from '../../_services/marketplace-server';
import { MarketplaceSkillDetail } from './_components/marketplace-skill-detail';
import { MarketplaceSkillLoadError } from './_components/marketplace-skill-load-error';

interface SkillDetailPageProps {
  params: Promise<{ marketplaceSlug: string; skillName: string }>;
}

export default async function SkillDetailPage({ params }: SkillDetailPageProps) {
  const { marketplaceSlug, skillName } = await params;
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <Stack gap="comfortable">
        <BackToSkillsLink />
        <Alert variant="error">
          <Icon name="lock" color="status-error" />
          <Text size="caption" color="status-error">
            Session expired. Please sign in again to view this Skill.
          </Text>
        </Alert>
      </Stack>
    );
  }

  const result = await loadSkillPage(session.accessToken, marketplaceSlug, skillName);

  let content: React.ReactNode;
  if (!result.ok && result.status === 404) {
    content = (
      <EmptyState
        icon="box"
        title="Skill not found"
        description="No Skill in this Marketplace matches this address."
        action={
          <Button asChild>
            <Link href={APP_ROUTES.marketplace.overview}>Back to Skills</Link>
          </Button>
        }
      />
    );
  } else if (!result.ok) {
    content = <MarketplaceSkillLoadError />;
  } else {
    content = (
      <MarketplaceSkillDetail skill={result.skill} repositoryUrl={result.marketplaceLink} />
    );
  }

  return (
    <Stack gap="comfortable">
      <BackToSkillsLink />
      {content}
    </Stack>
  );
}

function BackToSkillsLink() {
  // Sticky so the way back to the list stays in view while the Skill scrolls.
  return (
    <Stack
      direction="horizontal"
      paddingBlock="snug"
      style={{
        position: 'sticky',
        top: 0,
        zIndex: 20,
        background: 'var(--color-background)',
      }}
    >
      <Button variant="ghost" size="sm" asChild>
        <Link href={APP_ROUTES.marketplace.overview}>
          <Icon name="arrow-left" size="xs" />
          Skills
        </Link>
      </Button>
    </Stack>
  );
}
