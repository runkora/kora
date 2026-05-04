/**
 * @tier 3 — policy scoped.
 * @promote-to-tier-2 if: a second feature area outside policy adds a card/list view toggle.
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
      >
        <Icon name="layout-grid" size="2xs" />
      </Button>
      <Button
        variant={viewMode === 'list' ? 'default' : 'outline'}
        size="icon-sm"
        onClick={() => onToggle('list')}
        aria-label="List view"
      >
        <Icon name="list" size="2xs" />
      </Button>
    </ButtonGroup>
  );
}
