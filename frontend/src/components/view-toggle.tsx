/**
 * @tier 2 — App-level shared. Card/list view switch for a result list. Used by
 * policy (grants-tab, roles-tab) and marketplace (overview toolbar).
 * @promote-to-tier-1 if: a second non-dashboard Orbit surface needs a card/list view switch.
 */
import { Button, ButtonGroup, Icon } from '@orbit/terra';

export interface ViewToggleProps {
  viewMode: 'cards' | 'list';
  onToggle: (mode: 'cards' | 'list') => void;
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
