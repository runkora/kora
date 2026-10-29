/** @tier 3 — marketplace/[marketplaceSlug]/[skillName]-scoped. The GitHub repository a Skill comes from. */

import { Card, CardContent, IconTile, Stack, Text } from '@orbit/hades';

/** The `owner` and `owner/repo` of a GitHub repository link, or the raw link when it has no such path. */
export function repositoryName(link: string): { owner: string; fullName: string } {
  try {
    const [owner, repo] = new URL(link).pathname.split('/').filter(Boolean);
    if (owner && repo) return { owner, fullName: `${owner}/${repo.replace(/\.git$/, '')}` };
  } catch {
    // Not a URL; fall through to the raw link.
  }
  return { owner: link, fullName: link };
}

export function SkillRepositoryCard({ repositoryUrl }: { repositoryUrl: string }) {
  const { owner, fullName } = repositoryName(repositoryUrl);
  return (
    <Card column>
      <CardContent padded>
        <Stack direction="horizontal" align="center" gap="cozy">
          <IconTile size="lg" aria-hidden>
            <Text as="span" size="supporting" weight="heavy">
              {owner.charAt(0).toUpperCase()}
            </Text>
          </IconTile>
          <Stack grow minWidth="none" gap="none">
            <Text as="span" size="supporting" weight="heavy" truncate>
              {owner}
            </Text>
            <Text as="span" size="caption" color="subtle" truncate>
              {fullName}
            </Text>
          </Stack>
        </Stack>
      </CardContent>
    </Card>
  );
}
