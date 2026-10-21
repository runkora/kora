/**
 * @tier 2 — App-level shared layout control (no kora domain knowledge). Used by: policy (roles,
 * grants), agents, mcp-gateway.
 *
 * Switches a collection page between its card and table/list layouts.
 *
 * @promote-to-tier-1 if: a surface outside the dashboard needs it; it is a Hades candidate, which
 * needs human approval before any Hades change.
 */
import { Button, ButtonGroup, Icon } from '@runkora/hades';

export type ViewMode = 'cards' | 'list';

export interface ViewToggleProps {
  viewMode: ViewMode;
  onToggle: (mode: ViewMode) => void;
}

export function ViewToggle({ viewMode, onToggle }: ViewToggleProps) {
  return (
    <ButtonGroup>
      <Button
        variant={viewMode === 'cards' ? 'default' : 'outline'}
        size="icon-sm"
        onClick={() => onToggle('cards')}
        aria-label="Card view"
        aria-pressed={viewMode === 'cards'}
      >
        <Icon name="layout-grid" size="2xs" />
      </Button>
      <Button
        variant={viewMode === 'list' ? 'default' : 'outline'}
        size="icon-sm"
        onClick={() => onToggle('list')}
        aria-label="List view"
        aria-pressed={viewMode === 'list'}
      >
        <Icon name="list" size="2xs" />
      </Button>
    </ButtonGroup>
  );
}
