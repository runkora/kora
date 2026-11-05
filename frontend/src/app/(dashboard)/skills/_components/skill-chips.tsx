'use client';

/** @tier 3 — skills-scoped. The favourite heart, the copy-link button, the origin badge, the name and version line and the frontmatter chips that cards, rows and the skill page share. */

import {
  Badge,
  Button,
  Icon,
  Stack,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  useCopy,
  type IconName,
} from '@runkora/hades';
import { APP_ROUTES } from '@runkora/lib/routing';
import type { MarketplaceOrigin } from '@runkora/shared/api';

/** The folder between the skills root and the skill, as `engineering` in `skills/engineering/tdd`, in sentence case. */
function folderLabel(path: string): string | null {
  const parts = path.split('/');
  if (parts.length < 3) return null;
  const words = parts[parts.length - 2].replace(/[-_]/g, ' ');
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/** `pdf · v2.1.0`, or the name alone when the frontmatter declares no version. */
export function nameLine(skill: { name: string; version: string | null }): string {
  return skill.version ? `${skill.name} · v${skill.version}` : skill.name;
}

export function FavouriteButton({
  favourite,
  onToggle,
}: {
  favourite: boolean;
  onToggle: () => void;
}) {
  return (
    <Button
      variant="ghost"
      size="icon-sm"
      dim={!favourite}
      aria-label={favourite ? 'Remove from favourites' : 'Add to favourites'}
      aria-pressed={favourite}
      onClick={(event) => {
        // The card and the row open the skill on click; the heart must not.
        event.stopPropagation();
        onToggle();
      }}
    >
      <Icon name="heart" size="2xs" color={favourite ? 'rose' : 'muted'} filled={favourite} />
    </Button>
  );
}

/** Internal for the organisation's own content, External for third-party. */
export function OriginBadge({ origin }: { origin: MarketplaceOrigin }) {
  return (
    <Badge variant={origin === 'internal' ? 'secondary' : 'outline'} size="sm">
      {origin === 'internal' ? 'Internal' : 'External'}
    </Badge>
  );
}

/** Hades Text has no line clamp; the agent card's inline style is the precedent. */
export function clampLines(lines: number) {
  return {
    display: '-webkit-box',
    WebkitLineClamp: lines,
    WebkitBoxOrient: 'vertical',
    overflow: 'hidden',
  } as const;
}

/** The skill page's absolute address, without browse values, for sharing. */
export function skillUrl(skill: { marketplace: string; path: string }): string {
  const route = APP_ROUTES.skills.skill(skill.marketplace, skill.path);
  // Only read on click; the server render never copies.
  return typeof window === 'undefined' ? route : `${window.location.origin}${route}`;
}

/** An icon-only button, sized like the heart, that copies the skill page's address. */
export function CopyLinkButton({ skill }: { skill: { marketplace: string; path: string } }) {
  const { status, copy } = useCopy(skillUrl(skill));
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Copy link"
          onClick={(event) => {
            // The card opens the skill on click; copying must not.
            event.stopPropagation();
            void copy();
          }}
        >
          <Icon
            name={status === 'copied' ? 'check' : 'link'}
            size="2xs"
            color={status === 'copied' ? 'status-healthy' : 'muted'}
          />
        </Button>
      </TooltipTrigger>
      <TooltipContent>{status === 'copied' ? 'Copied' : 'Copy link'}</TooltipContent>
    </Tooltip>
  );
}

function MetaBadge({ icon, children }: { icon: IconName; children: React.ReactNode }) {
  return (
    <Badge variant="secondary" size="sm">
      <Icon name={icon} size="2xs" />
      {children}
    </Badge>
  );
}

/** Chips from the skill's folder and frontmatter only; nothing shows when the skill declares nothing. */
export function SkillChips({
  skill,
}: {
  skill: { path: string; manualOnly: boolean; tags: string[]; tools: string[] };
}) {
  const folder = folderLabel(skill.path);
  if (!folder && !skill.manualOnly && skill.tags.length === 0 && skill.tools.length === 0) {
    return null;
  }
  return (
    <Stack direction="horizontal" wrap align="center" gap="tight">
      {folder && <MetaBadge icon="folder">{folder}</MetaBadge>}
      {skill.manualOnly && <MetaBadge icon="user">Manual only</MetaBadge>}
      {skill.tags.map((tag) => (
        <Badge key={`tag:${tag}`} variant="secondary" size="sm">
          {tag}
        </Badge>
      ))}
      {skill.tools.map((tool) => (
        <Badge key={`tool:${tool}`} variant="elevated" size="sm" mono>
          {tool}
        </Badge>
      ))}
    </Stack>
  );
}
