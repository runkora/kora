/** @tier 1 — Controlled card/list view switch with no application domain knowledge. */
import * as React from 'react';
import { cva } from 'class-variance-authority';
import { cn } from '../../utils';
import { Button } from '../button';
import { ButtonGroup, type ButtonGroupProps } from '../button-group';
import { Icon } from '../icon';

const viewToggleVariants = cva('inline-flex');

export interface ViewToggleProps extends Omit<ButtonGroupProps, 'children'> {
  viewMode: 'cards' | 'list';
  onToggle: (mode: 'cards' | 'list') => void;
  className?: string;
}

export const ViewToggle = React.forwardRef<HTMLDivElement, ViewToggleProps>(
  ({ viewMode, onToggle, className, ...props }, ref) => (
    <ButtonGroup
      ref={ref}
      className={cn(viewToggleVariants(), className)}
      data-slot="view-toggle"
      {...props}
    >
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
  ),
);
ViewToggle.displayName = 'ViewToggle';
