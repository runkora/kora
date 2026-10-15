'use client';

/** @tier 3 — marketplace-scoped. The open Skill in place of the results: loading, missing, failed, or loaded. */

import { useEffect, useRef } from 'react';
import { Button, EmptyState, Icon, Stack } from '@orbit/hades';
import { useMarketplaceContext } from '../_context/marketplace-context';
import { MarketplaceSkillDetail } from './marketplace-skill-detail';
import { MarketplaceSkillDetailSkeleton } from './marketplace-skill-detail-skeleton';
import { MarketplaceSkillLoadError } from './marketplace-skill-load-error';

/**
 * Shows the Skill that the user opened. The address stays `/marketplace`.
 * "Skills" closes the Skill, and the overview shows the same results again.
 */
export function MarketplaceSkillView() {
  const { openSlug, skill, skillStatus, marketplaceName, closeSkill, retrySkill } =
    useMarketplaceContext();
  const backRef = useRef<HTMLButtonElement>(null);

  // The results are gone from the page. Move keyboard focus to the control
  // that brings them back, so focus does not fall to the document body.
  useEffect(() => {
    backRef.current?.focus();
  }, [openSlug]);

  let content: React.ReactNode;
  if (skillStatus === 'success' && skill) {
    content = <MarketplaceSkillDetail skill={skill} marketplaceName={marketplaceName} />;
  } else if (skillStatus === 'not_found') {
    // No Skill has this slug, for example after a catalogue change.
    content = (
      <EmptyState
        icon="box"
        title="Skill not found"
        description="This Skill is no longer in the Marketplace."
        action={<Button onClick={closeSkill}>Back to Skills</Button>}
      />
    );
  } else if (skillStatus === 'error') {
    // The load failed. This is not a missing Skill. The notice has a retry.
    content = <MarketplaceSkillLoadError onRetry={retrySkill} />;
  } else {
    // The request is in flight.
    content = <MarketplaceSkillDetailSkeleton />;
  }

  return (
    <Stack gap="comfortable">
      <Stack direction="horizontal">
        <Button ref={backRef} variant="ghost" size="sm" onClick={closeSkill}>
          <Icon name="arrow-left" size="xs" />
          Skills
        </Button>
      </Stack>
      {content}
    </Stack>
  );
}
