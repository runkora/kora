import * as React from 'react';
import { cn } from '../../utils';
import { GAP } from '../layout/spacing';
import type { SpacingSize } from '../layout/spacing';

export interface ListProps {
  children: React.ReactNode;
  /** Renders `<ol>` if true, `<ul>` if false (default). */
  ordered?: boolean;
  gap?: SpacingSize;
  /** When true, adds a hairline border between list items (`divide-y`). */
  separator?: boolean;
}

/**
 * List
 *
 * A semantic list container that renders as `<ul>` (default) or `<ol>` when
 * `ordered` is set. Uses a flex column layout with token-aligned gap.
 *
 * ```tsx
 * <List>
 *   <ListItem>First item</ListItem>
 *   <ListItem>Second item</ListItem>
 * </List>
 *
 * <List ordered>
 *   <ListItem>Step one</ListItem>
 *   <ListItem>Step two</ListItem>
 * </List>
 * ```
 */
const List = React.forwardRef<HTMLUListElement | HTMLOListElement, ListProps>(
  ({ children, ordered = false, gap, separator = false }, ref) => {
    const Tag = ordered ? 'ol' : 'ul';
    return (
      <Tag
        ref={ref as React.Ref<HTMLUListElement & HTMLOListElement>}
        className={cn(
          'list-none m-0 p-0 flex flex-col',
          gap && GAP.base[gap],
          separator && 'divide-y divide-border-subtle',
        )}
      >
        {children}
      </Tag>
    );
  },
);

List.displayName = 'List';

export interface ListItemProps {
  children: React.ReactNode;
}

/**
 * ListItem
 *
 * A semantic `<li>` element. Children handle their own layout.
 *
 * ```tsx
 * <ListItem>
 *   <Stack direction="horizontal" align="center" gap="snug">
 *     <Icon name="check" />
 *     <Text>Task complete</Text>
 *   </Stack>
 * </ListItem>
 * ```
 */
const ListItem = React.forwardRef<HTMLLIElement, ListItemProps>(({ children }, ref) => {
  return <li ref={ref}>{children}</li>;
});

ListItem.displayName = 'ListItem';

export { List, ListItem };
