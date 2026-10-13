# Golden Path: Laying Out a Table

**Audience:** Frontend engineers building or fixing a `DataTable` / `SmartTable`.
**Origin:** Derived from reworking the Connections table (`src/app/(dashboard)/connections/`), which is the reference implementation. Treat the rules as a working hypothesis until a few more tables have applied them.

The recurring failures this guide prevents: columns that collapse to a few pixels, text or chips that bleed into the next column, a table wider than its card, and rows that grow without bound.

---

## What `DataTable` already does for you

Every `DataTable` gets these without any per-table work:

- **Cells clip.** Overflowing cell content is clipped with an ellipsis, and a truncated cell shows its full text as a native tooltip on hover. Opt a column out with `wrap: true`.
- **Flexible columns have a floor.** A column sized with `fr` never shrinks below `minWidth` (default `5rem`), so it cannot collapse.
- **Headers wrap** instead of clipping their first letters.
- **Column titles are always in capitals.** Pass `header` in any casing; the table draws every title (sortable ones included) in the same uppercase overline style. Do not upper-case the string yourself and do not style headers by hand.
- **Row actions can be pinned to the right.** Give the actions column `pinEnd: true`. When the table scrolls, every other column slides underneath it, so it is never pushed off screen. Every row-actions column in the app sets it. A pinned cell is solid across the whole row, padding included, so nothing shows through around its content. A pinned column gets an edge line and a shadow only while scrolled content is actually underneath it: the right pin while there is more to scroll to the right, the left pin once scrolled away from the start. The line is the subtle border colour. In the dark theme the pinned cell also takes the elevated surface tone, because a shadow barely shows on a near-black surface; in the light theme the shadow is soft and the tone is unchanged. The actions content is centred between the line and the table edge.
- **The scrollbar is always visible.** A scrolling table draws a persistent scrollbar (the default overlay scrollbar on macOS stays hidden until you scroll, which gives no hint that there is more). Firefox ignores the styling and uses its own scrollbar.
- **Horizontal scrolling is the last resort.** The header and body scroll together inside the table, and only once the container is narrower than every column at its minimum width. The page never scrolls sideways.
- **Columns can opt in to a filter.** `filter.searchable` adds a search box and a scrollable, multi-select list to the filter popover.

Tables built from the raw primitives (`Table`, `TableRow`, `TableCell`) get none of this. Prefer `DataTable`, or apply the rules below by hand. A raw table lays out its columns automatically, so a chip's unbroken text sets its column's minimum width and a header width is ignored. Set `maxWidth: 0` on the chip's cell and a width share on the header.

---

## Rule 1: Declare every column's width

The default track is `1fr`. Equal shares give a narrow column too much room and a wide one too little. Set `gridWidth` explicitly on every column and let only columns that genuinely should share leftover space flex.

Declaring a flexible column is fine and expected: write `gridWidth: 'minmax(0, 2fr)'` with a `minWidth`. What to avoid is leaving the default in place by accident.

A fixed column with a `minWidth` becomes `minmax(minWidth, gridWidth)`: it takes its full size when there is room and shrinks toward the minimum only when the viewport is too narrow.

## Rule 2: Classify each column, then size it by class

| Class                | Examples               | Size                                                                                        |
| -------------------- | ---------------------- | ------------------------------------------------------------------------------------------- |
| Identifier           | Name, slug, run ID     | Fixed: the longest realistic value plus 50%. Shrinks toward a minimum. Truncates past that. |
| Enum or badge        | Adapter, status, type  | Fixed: the widest known value plus 50%.                                                     |
| Number, date, status | Cost, created, latency | Fixed and snug. Non-wrapping. Right-align numbers.                                          |
| Collection           | Scopes, used by, tags  | Flexible (`fr`), clamped to two rows (Rule 8).                                              |
| Actions              | Edit, delete           | Fixed and small.                                                                            |

"Plus 50%" is a judgement call about headroom. Use the real longest value you know of. Connections sizes Name for `constellation-kubernetes` plus 50%.

## Rule 3: Measure, do not guess

Read real rendered widths in the browser and scale from them:

- Mono text at 14px measured about 8.4px per character, so a 24-character name is about 202px.
- A badge is its text width plus about 14px of padding and border.

## Rule 4: Check the width budget

The minimum widths plus the gaps and the row padding must fit inside the card:

```
sum(min widths) + 14px x (columns - 1) + 32px  <=  card width
```

Hades' grid uses a 14px gap between columns and 16px of padding on each side of a row. Design against iPad Air landscape (1180 x 820), where the card is about 930px wide. Every overflow found while writing this guide broke this sum. If the sum does not fit, reduce minimums or accept that columns shrink; do not let the table extend past the card.

## Rule 5: Tables are full width

Do not cap, centre or shrink-wrap a table. Leftover space goes to the flexible columns. Small screens are expected to get a separate card layout.

## Rule 6: Scrolling is the last resort, and columns never hide

When the container is too narrow for every column at its minimum width (the Rule 4 sum), the table scrolls horizontally. The scrolling belongs to the table, not the page: the toolbar and pagination stay put, and the header row scrolls with the body.

- Do not hide columns to make room, and do not add per-column drop rules. Every column stays available.
- Because columns shrink to their minimums first, scrolling starts only after all of them have reached them. Good minimums are therefore what keep a table from scrolling at the widths you design for.
- The primary identifier pins to the left (the first `locked` column) and the row-actions column pins to the right (`pinEnd`), so both stay in view while the rest scrolls underneath. Any other column is reached by scrolling.

## Rule 7: Anything that can grow must truncate and show its full value

- **Plain text** truncates and shows the tooltip automatically.
- **Badges and chips** need a definite width to truncate against. Use `TruncatingBadge` (`@orbit/hades`) or `ScopeChip` (`@orbit/components`). They give the badge a stretched, shrinkable wrapper and a truncating text span, with the full label as the tooltip.
- **Never put a raw badge directly in a block-level cell.** It stretches to the cell's full width. Put it in a horizontal `Stack` (which `TruncatingBadge` assumes).
- **Do not add truncation to a Hades primitive** such as `Badge`. The concern belongs in the component that composes it.

## Rule 8: Lists in cells clamp to two rows

For a column holding many items (scopes, agents), show as many as fit in two rows, then a "+N more" button that opens an overlay listing everything.

- Use `ScopeChipList` for scopes (`maxRows={2}`) or `ClampedList` directly for anything else, with the badge you want in `renderItem`. Both wrap `useClampedRows`. A small feature-scoped list such as `UsedByList` or the model gateway's `BadgeList` is `ClampedList` with one fixed `renderItem`. The count depends on the width the column gets, so it is measured, not counted.
- The "+N more" trigger must always fit. When the list overflows, each item is capped narrower so the trigger, with up to a three-digit count (`+999 more`), can share the last row. One wide chip can therefore never push the trigger out. The cap never drops below 60% of the cell, so in a narrow cell badges are not squeezed to a few letters; items are dropped until the trigger fits instead.
- Add a searchable multi-select filter to the column. Selecting several values keeps any row with at least one of them.
- Keep the row height bounded. Never render an unbounded stack of chips.

## Rule 9: Clickable rows need click isolation

When a table has `onRowClick`, stop click propagation on the "+N more" trigger and on the overlay content. React events bubble through portals, so without this a click inside the overlay also fires the row's action.

## Rule 10: Identity cells show a name, not a repeated ID

Show the human name as the main text and the machine ID as a smaller line beneath it. Do not add a separate ID column that repeats the same value.

The part of an identifier that tells two rows apart must be visible in the cell. If every value shares a long prefix, show the distinguishing part as the main text rather than relying on truncation, which cuts from the end. When the prefix is already shown elsewhere in the cell (a family label above a `family/name` id), drop it from the id line. When an identifier cannot be shortened and its distinguishing part is at the end, set `wrap: true` on the column and let it break anywhere (`overflowWrap: 'anywhere'`) instead of truncating.

A label that carries its own distinguishing part, such as `AWS Bedrock (aus)`, gets a minimum width that fits its widest value; truncating it drops the part that matters.

A principal's type is its own column, drawn with `PrincipalBadge`, not a badge beside the name.

## Rule 11: A table whose rows are truncated needs a details view

Anything the table clamps or truncates (scopes, IDs) must be readable in full somewhere. Make the row clickable (`onRowClick`) and open a detail modal with the whole record, as Connections (`ConnectionDetailModal`) and Grants (`GrantDetailModal`) do. Controls inside the row, such as delete, must stop propagation so they do not also open it.

---

## Verifying a table

Do this at two widths, with numbers, not by eye alone.

1. **Desktop** (the normal window) and **iPad Air landscape**: render the page in a 1180 x 820 iframe so the responsive breakpoints see the right width.
2. Read the cell widths and check for horizontal overflow. In the browser console, inside the iframe document:

   ```js
   const d = document.getElementById('f').contentDocument; // the 1180 x 820 iframe
   const rows = [...d.querySelectorAll('[role=row]')].slice(1);
   const table = d.querySelector('[role=table]').getBoundingClientRect();
   ({
     table: Math.round(table.width),
     overflowX: d.documentElement.scrollWidth > d.documentElement.clientWidth,
     cells: rows.map((r) =>
       [...r.querySelectorAll('[role=cell]')].map((c) =>
         Math.round(c.getBoundingClientRect().width),
       ),
     ),
   });
   ```

3. **Use adversarial data.** Long names, very long identifiers, and five or more items in every collection column.
4. Check that truncated cells show their full value on hover, and that the "+N more" overlay opens without triggering the row's click.

## Where the pieces live

| Piece                                                                                                | Location                                                                                  |
| ---------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| `ScopeChip`, `ScopeChipList`, `ClampedList`, `scopeColumnFilter`, `PrincipalBadge`, `useClampedRows` | `src/components/` (import from `@orbit/components`)                                       |
| `TruncatingBadge`                                                                                    | Hades: `src/packages/hades/src/components/truncating-badge/` (import from `@orbit/hades`) |
| Cell clipping, minimum widths, searchable filter                                                     | `DataTable` in `src/packages/hades/src/components/data-table/`                            |
| Reference table                                                                                      | `src/app/(dashboard)/connections/_components/connections-page.tsx`                        |
| Feature-scoped clamped list                                                                          | `src/app/(dashboard)/connections/_components/used-by-list.tsx`                            |

## Worked example: Connections

| Column            | Class         | Sizing                                                                            |
| ----------------- | ------------- | --------------------------------------------------------------------------------- |
| Name              | Identifier    | `19rem` (`constellation-kubernetes` plus 50%), minimum `8rem`                     |
| Adapter           | Enum or badge | `8.5rem` (the `Kubernetes` badge plus headroom), minimum `6rem`, truncating badge |
| Grantable actions | Collection    | `minmax(0, 2fr)`, minimum `16rem`, two rows, searchable filter                    |
| Used by           | Collection    | `minmax(0, 2fr)`, minimum `14rem`, two rows, searchable filter                    |

As the table narrows, the two collection columns show fewer items and the rest sit behind "+N more".

## Not yet decided

Rules that came up while applying this guide and have not been settled are tracked here once they are reviewed, so that this guide stays a record of decisions rather than guesses.

- Whether an identifier shared by many rows should ever truncate from the middle or start instead of the end.
- How to size an identifier column whose values are long but structured (Rule 2 assumes a knowable maximum).
