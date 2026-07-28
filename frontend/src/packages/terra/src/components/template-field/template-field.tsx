'use client';

import * as React from 'react';
import { cn } from '../../utils';
import { Button } from '../button';
import { Icon } from '../icon';
import { Popover, PopoverContent, PopoverTrigger } from '../popover';
import { SectionLabel } from '../section-label';
import { Text } from '../typography';
import { TemplateCatalogList } from './template-catalog-list';
import {
  TOKEN_SLOT,
  deleteBackward,
  deleteForward,
  deleteRange,
  domPositionToModelOffset,
  exampleMap,
  insertAt,
  modelOffsetToDom,
  normalizeTemplate,
  parseTemplate,
  readModelFromDom,
  renderPreview,
  serializeToken,
  type TemplateCatalogEntry,
} from './template-model';

export type { TemplateCatalogEntry } from './template-model';

// ── Props ─────────────────────────────────────────────────────────────────────

export interface TemplateFieldProps {
  /** The raw template string, e.g. `Review {{ pr.title }}`. */
  value: string;
  /** Called with the raw template string on every edit. */
  onChange: (value: string) => void;
  /** Every field the template may reference. Drives pills, picker, and preview. */
  catalog: readonly TemplateCatalogEntry[];
  /** Shown when the template is empty. */
  placeholder?: string;
  /** Id for the editable region, for an external `<Label htmlFor>`. */
  id?: string;
  /** Accessible name, when no visible label is associated. */
  'aria-label'?: string;
  /** Makes the editor read-only and dims the whole frame. */
  disabled?: boolean;
  /** Rows-worth of minimum editor height. Defaults to 4. */
  minRows?: number;
  /**
   * One line naming what the catalog is scoped to, shown in the picker, e.g.
   * "Fields for push deliveries". Makes a narrowed catalog say why.
   */
  catalogLabel?: string;
  /** @internal Used for internal Terra composition via cn(). */
  className?: string;
}

// ── Pill rendering ────────────────────────────────────────────────────────────

const PILL_BASE =
  'mx-px inline-block select-none rounded-sm border px-1.5 align-baseline font-mono text-[11.5px] leading-[1.35] whitespace-nowrap';

/** A token the catalog knows about: accent tint, the same family as a Badge. */
const PILL_KNOWN = 'border-accent/30 bg-accent/15 text-accent';

/**
 * A token the catalog does not know about. It is not an error the person must
 * fix before saving — the backend simply renders it empty — but it should look
 * wrong, because it almost always is a typo.
 */
const PILL_UNKNOWN = 'border-status-error/30 bg-status-error/15 text-status-error';

const UNKNOWN_TITLE = 'Unknown field: renders empty';

function buildPill(doc: Document, token: string, known: boolean): HTMLElement {
  const el = doc.createElement('span');
  el.setAttribute('data-slot', TOKEN_SLOT);
  el.setAttribute('data-token', token);
  el.setAttribute('data-known', known ? 'true' : 'false');
  el.setAttribute('contenteditable', 'false');
  el.setAttribute('aria-label', `field ${token}`);
  el.setAttribute('class', cn(PILL_BASE, known ? PILL_KNOWN : PILL_UNKNOWN));
  if (!known) el.setAttribute('title', UNKNOWN_TITLE);
  el.textContent = token;
  return el;
}

/** The deterministic DOM render of a template string. */
function buildNodes(doc: Document, value: string, known: ReadonlySet<string>): Node[] {
  return parseTemplate(value).map((part) =>
    part.kind === 'text'
      ? doc.createTextNode(part.text)
      : buildPill(doc, part.token, known.has(part.token)),
  );
}

// ── Selection helpers ─────────────────────────────────────────────────────────

interface ModelSelection {
  start: number;
  end: number;
}

/**
 * The current selection as model offsets, or null when there is no usable
 * selection. Returns null rather than guessing so callers can decline to
 * intercept a key they cannot place.
 */
function readSelection(root: HTMLElement): ModelSelection | null {
  const view = root.ownerDocument?.defaultView;
  const selection = view?.getSelection?.();
  if (!selection || selection.rangeCount === 0) return null;
  const { anchorNode, anchorOffset, focusNode, focusOffset } = selection;
  if (!anchorNode || !focusNode) return null;
  if (!root.contains(anchorNode) || !root.contains(focusNode)) return null;
  const a = domPositionToModelOffset(root, anchorNode, anchorOffset);
  const b = domPositionToModelOffset(root, focusNode, focusOffset);
  return { start: Math.min(a, b), end: Math.max(a, b) };
}

/** Place the caret at a model offset. Best-effort: no-op where unsupported. */
function placeCaret(root: HTMLElement, offset: number): void {
  const doc = root.ownerDocument;
  const view = doc?.defaultView;
  const selection = view?.getSelection?.();
  if (!selection || typeof doc?.createRange !== 'function') return;
  try {
    const position = modelOffsetToDom(root, offset);
    const range = doc.createRange();
    range.setStart(position.node, position.offset);
    range.collapse(true);
    selection.removeAllRanges();
    selection.addRange(range);
  } catch {
    // happy-dom and some browsers reject positions in a just-replaced tree.
    // A missing caret is recoverable; a thrown error inside an input handler
    // is not.
  }
}

// ── TemplateField ─────────────────────────────────────────────────────────────

/**
 * TemplateField
 *
 * A prompt-template editor for strings containing `{{ dotted.token }}`
 * placeholders. The value in and out is always the raw template string; inside
 * the editor each placeholder is drawn as an atomic pill showing only the
 * token, so a person writes
 *
 *   Review {{ pr.title }} on {{ repo.full_name }}
 *
 * while reading `Review [pr.title] on [repo.full_name]`.
 *
 * A footer bar attached inside the same frame carries an "Insert field" picker
 * over the catalog, and a "Preview" toggle that substitutes each field's sample
 * value — including the backend's rule that an unknown field renders empty.
 *
 * Typing a complete `{{ token }}` as plain text turns it into a pill; deleting
 * next to a pill removes the whole token, never half of it.
 *
 * ```tsx
 * <TemplateField
 *   value={prompt}
 *   onChange={setPrompt}
 *   aria-label="Prompt template"
 *   catalog={[
 *     { token: 'pr.title', description: 'Pull request title', example: 'Fix login retry' },
 *   ]}
 * />
 * ```
 *
 * **Uncontrolled while focused.** The editable region is a contentEditable
 * whose DOM is re-rendered from the model on every keystroke. While it has
 * focus it owns its own content, and a `value` prop that disagrees with the
 * last emitted string is ignored until focus leaves; otherwise the caret would
 * jump under the person's hands on every round trip. Blur, and the prop wins
 * again. Treat `onChange` as authoritative and do not transform `value` in the
 * parent mid-edit.
 */
export const TemplateField = React.forwardRef<HTMLDivElement, TemplateFieldProps>(
  (
    {
      value,
      onChange,
      catalog,
      catalogLabel,
      placeholder,
      id,
      'aria-label': ariaLabel,
      disabled = false,
      minRows = 4,
      className,
    },
    ref,
  ) => {
    const editorRef = React.useRef<HTMLDivElement>(null);
    /** The last model string this component rendered or emitted. */
    const modelRef = React.useRef(value);
    /** Where the caret was when the editor last had it — the insertion point. */
    const caretRef = React.useRef(value.length);
    const renderedRef = React.useRef(false);
    const renderedCatalogRef = React.useRef('');

    const [pickerOpen, setPickerOpen] = React.useState(false);
    const [previewOn, setPreviewOn] = React.useState(false);

    const knownTokens = React.useMemo(
      () => new Set(catalog.filter((e) => !e.pattern).map((e) => e.token)),
      [catalog],
    );
    const catalogKey = React.useMemo(() => [...knownTokens].join(' '), [knownTokens]);
    const examples = React.useMemo(() => exampleMap(catalog), [catalog]);
    const hasExamples = examples.size > 0;

    /** Replace the editor DOM with the render of `next`, then restore the caret. */
    const paint = React.useCallback(
      (next: string, caret: number | null) => {
        const root = editorRef.current;
        if (!root) return;
        const doc = root.ownerDocument;
        root.replaceChildren(...buildNodes(doc, next, knownTokens));
        renderedRef.current = true;
        renderedCatalogRef.current = catalogKey;
        if (caret !== null) {
          caretRef.current = caret;
          placeCaret(root, caret);
        }
      },
      [catalogKey, knownTokens],
    );

    /** Adopt a new model: repaint, remember it, and tell the parent. */
    const commit = React.useCallback(
      (next: string, caret: number | null) => {
        modelRef.current = next;
        paint(next, caret);
        onChange(next);
      },
      [onChange, paint],
    );

    // Controlled sync. While focused the editor owns its content (see the
    // component doc); otherwise the prop is the truth and we repaint from it.
    React.useEffect(() => {
      const root = editorRef.current;
      if (!root) return;
      const doc = root.ownerDocument;
      const focused = doc.activeElement === root || root.contains(doc.activeElement);
      const stale = !renderedRef.current || renderedCatalogRef.current !== catalogKey;
      if (!stale && value === modelRef.current) return;
      if (focused && value !== modelRef.current) return;
      modelRef.current = value;
      paint(value, null);
    }, [value, catalogKey, paint]);

    // ── Input ────────────────────────────────────────────────────────────────

    const handleInput = React.useCallback(() => {
      const root = editorRef.current;
      if (!root || disabled) return;
      const raw = readModelFromDom(root);
      const selection = readSelection(root);
      const rawCaret = selection ? selection.end : raw.length;
      const { value: next, caret } = normalizeTemplate(raw, rawCaret);
      commit(next, caret);
    }, [commit, disabled]);

    /** Apply a model edit driven by a key we intercepted. */
    const applyEdit = React.useCallback(
      (edit: { value: string; caret: number }) => {
        if (edit.value === modelRef.current) {
          paint(edit.value, edit.caret);
          return;
        }
        commit(edit.value, edit.caret);
      },
      [commit, paint],
    );

    const handleKeyDown = React.useCallback(
      (event: React.KeyboardEvent<HTMLDivElement>) => {
        const root = editorRef.current;
        if (!root || disabled) return;
        const model = modelRef.current;

        if (event.key === 'Enter') {
          // Insert a real newline rather than letting the browser build a
          // <div>/<br> tree the model would have to reverse-engineer.
          event.preventDefault();
          const selection = readSelection(root);
          const start = selection ? selection.start : model.length;
          const end = selection ? selection.end : model.length;
          const cleared =
            start === end ? { value: model, caret: start } : deleteRange(model, start, end);
          applyEdit(insertAt(cleared.value, cleared.caret, '\n'));
          return;
        }

        if (event.key === 'Backspace' || event.key === 'Delete') {
          const selection = readSelection(root);
          if (!selection) return; // No usable selection: let the browser try.
          event.preventDefault();
          if (selection.start !== selection.end) {
            applyEdit(deleteRange(model, selection.start, selection.end));
          } else if (event.key === 'Backspace') {
            applyEdit(deleteBackward(model, selection.start));
          } else {
            applyEdit(deleteForward(model, selection.start));
          }
        }
      },
      [applyEdit, disabled],
    );

    const handlePaste = React.useCallback(
      (event: React.ClipboardEvent<HTMLDivElement>) => {
        const root = editorRef.current;
        if (!root || disabled) return;
        event.preventDefault();
        const text = event.clipboardData?.getData('text/plain') ?? '';
        if (!text) return;
        const model = modelRef.current;
        const selection = readSelection(root);
        const start = selection ? selection.start : caretRef.current;
        const end = selection ? selection.end : caretRef.current;
        const cleared =
          start === end ? { value: model, caret: start } : deleteRange(model, start, end);
        applyEdit(insertAt(cleared.value, cleared.caret, text));
      },
      [applyEdit, disabled],
    );

    /** Remember the caret so the picker knows where to insert after blur. */
    const rememberCaret = React.useCallback(() => {
      const root = editorRef.current;
      if (!root) return;
      const selection = readSelection(root);
      if (selection) caretRef.current = selection.end;
    }, []);

    const insertToken = React.useCallback(
      (token: string) => {
        setPickerOpen(false);
        const root = editorRef.current;
        if (!root || disabled) return;
        root.focus();
        const edit = insertAt(modelRef.current, caretRef.current, serializeToken(token));
        commit(edit.value, edit.caret);
      },
      [commit, disabled],
    );

    // ── Chrome ───────────────────────────────────────────────────────────────

    const isEmpty = value.length === 0;
    // text-sm is 14px at leading-[1.5]; the frame adds py-2 top and bottom.
    const minHeight = `${Math.max(1, minRows) * 21 + 16}px`;
    const preview = React.useMemo(() => renderPreview(value, examples), [value, examples]);

    return (
      <div
        ref={ref}
        data-slot="template-field"
        className={cn(
          'flex w-full flex-col overflow-hidden rounded-md border border-border bg-surface-elevated',
          'focus-within:border-accent transition-colors',
          disabled && 'cursor-not-allowed opacity-40',
          className,
        )}
      >
        <div className="relative">
          {isEmpty && placeholder ? (
            <span
              aria-hidden="true"
              data-slot="template-field-placeholder"
              className="pointer-events-none absolute left-3 top-2 text-sm leading-[1.5] text-subtle"
            >
              {placeholder}
            </span>
          ) : null}
          <div
            ref={editorRef}
            id={id}
            role="textbox"
            aria-multiline="true"
            aria-label={ariaLabel}
            aria-disabled={disabled || undefined}
            data-slot="template-field-editor"
            contentEditable={!disabled}
            suppressContentEditableWarning
            spellCheck={false}
            style={{ minHeight }}
            onInput={handleInput}
            onKeyDown={handleKeyDown}
            onPaste={handlePaste}
            onKeyUp={rememberCaret}
            onMouseUp={rememberCaret}
            onBlur={rememberCaret}
            className={cn(
              'w-full whitespace-pre-wrap break-words px-3 py-2',
              'text-sm leading-[1.5] font-normal text-foreground',
              'focus-visible:outline-none',
              disabled && 'pointer-events-none',
            )}
          />
        </div>

        <div
          data-slot="template-field-toolbar"
          className="flex items-center gap-1 border-t border-border bg-surface-overlay px-2 py-1.5"
        >
          <Popover open={pickerOpen} onOpenChange={setPickerOpen}>
            <PopoverTrigger asChild>
              <Button
                variant="ghost"
                size="xs"
                type="button"
                disabled={disabled}
                aria-label="Insert field"
                data-slot="template-field-insert"
              >
                <Icon name="braces" size="2xs" />
                Insert field
              </Button>
            </PopoverTrigger>
            <PopoverContent align="start" className="p-1.5">
              <TemplateCatalogList
                catalog={catalog}
                contextLabel={catalogLabel}
                onSelect={insertToken}
              />
            </PopoverContent>
          </Popover>

          {hasExamples ? (
            <Button
              variant="ghost"
              size="xs"
              type="button"
              disabled={disabled}
              aria-pressed={previewOn}
              data-slot="template-field-preview-toggle"
              onClick={() => setPreviewOn((on) => !on)}
            >
              <Icon name={previewOn ? 'eye-off' : 'eye'} size="2xs" />
              Preview
            </Button>
          ) : null}
        </div>

        {hasExamples && previewOn ? (
          <div
            data-slot="template-field-preview"
            className="flex flex-col gap-1 border-t border-border px-3 py-2"
          >
            <SectionLabel>Example</SectionLabel>
            <Text size="supporting" color="muted" className="whitespace-pre-wrap break-words">
              {preview}
            </Text>
          </div>
        ) : null}
      </div>
    );
  },
);

TemplateField.displayName = 'TemplateField';
