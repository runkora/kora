import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { TemplateField } from './template-field';
import { TemplateCatalogList } from './template-catalog-list';
import type { TemplateCatalogEntry } from './template-model';

/**
 * happy-dom has no editing engine and only a stub Selection, so these tests
 * never rely on a real caret. Anything that would need one is driven the way
 * the component itself reads the world: mutate the contentEditable DOM, fire
 * the event a browser would fire, and assert on the template string that comes
 * back out. The caret arithmetic itself is covered exhaustively against the
 * pure functions in template-model.test.ts.
 */

const CATALOG: TemplateCatalogEntry[] = [
  {
    token: 'pr.title',
    description: 'Pull request title',
    example: 'Fix login retry',
    group: 'Pull request',
  },
  { token: 'pr.number', description: 'Pull request number', example: '412', group: 'Pull request' },
  { token: 'repo.full_name', description: 'Owner and repository name' },
  { token: 'group.labels.<name>', description: 'Any label on the group', pattern: true },
];

function editorOf(container: HTMLElement): HTMLElement {
  const editor = container.querySelector<HTMLElement>('[data-slot="template-field-editor"]');
  if (!editor) throw new Error('editor not found');
  return editor;
}

/** Replace the editor's content the way a browser would after typing. */
function typeInto(editor: HTMLElement, text: string): void {
  editor.replaceChildren(editor.ownerDocument.createTextNode(text));
  fireEvent.input(editor);
}

describe('TemplateField', () => {
  it('renders literal text and an atomic pill per token', () => {
    const { container } = render(
      <TemplateField
        value="Review {{ pr.title }} now"
        onChange={vi.fn()}
        catalog={CATALOG}
        aria-label="Prompt"
      />,
    );
    const editor = editorOf(container);
    const pills = editor.querySelectorAll('[data-slot="template-token"]');
    expect(pills).toHaveLength(1);
    expect(pills[0]).toHaveAttribute('data-token', 'pr.title');
    expect(pills[0]).toHaveAttribute('contenteditable', 'false');
    // The pill shows the token only; the braces never reach the screen.
    expect(pills[0].textContent).toBe('pr.title');
    expect(editor.textContent).toBe('Review pr.title now');
  });

  it('labels each pill for assistive technology', () => {
    const { container } = render(
      <TemplateField value="{{ pr.title }}" onChange={vi.fn()} catalog={CATALOG} />,
    );
    expect(editorOf(container).querySelector('[data-slot="template-token"]')).toHaveAttribute(
      'aria-label',
      'field pr.title',
    );
  });

  it('gives a token outside the catalog the error treatment', () => {
    const { container } = render(
      <TemplateField value="{{ pr.nope }}" onChange={vi.fn()} catalog={CATALOG} />,
    );
    const pill = editorOf(container).querySelector('[data-slot="template-token"]');
    expect(pill).toHaveAttribute('data-known', 'false');
    expect(pill).toHaveAttribute('title', 'Unknown field: renders empty');
    expect(pill?.getAttribute('class')).toContain('text-status-error');
  });

  it('treats a listed pattern family as unknown, since it never resolves', () => {
    const { container } = render(
      <TemplateField value="{{ group.labels }}" onChange={vi.fn()} catalog={CATALOG} />,
    );
    expect(editorOf(container).querySelector('[data-slot="template-token"]')).toHaveAttribute(
      'data-known',
      'false',
    );
  });

  it('is a multiline textbox wired to its label and id', () => {
    render(
      <TemplateField value="" onChange={vi.fn()} catalog={CATALOG} id="tpl" aria-label="Prompt" />,
    );
    const editor = screen.getByRole('textbox', { name: 'Prompt' });
    expect(editor).toHaveAttribute('aria-multiline', 'true');
    expect(editor).toHaveAttribute('id', 'tpl');
  });

  it('emits the raw template string when plain text is typed', () => {
    const onChange = vi.fn();
    const { container } = render(
      <TemplateField value="" onChange={onChange} catalog={CATALOG} aria-label="Prompt" />,
    );
    typeInto(editorOf(container), 'Review this');
    expect(onChange).toHaveBeenCalledWith('Review this');
  });

  it('turns a typed-out placeholder into a pill and canonicalises the string', () => {
    const onChange = vi.fn();
    const { container } = render(
      <TemplateField value="" onChange={onChange} catalog={CATALOG} aria-label="Prompt" />,
    );
    const editor = editorOf(container);
    typeInto(editor, 'Review {{pr.title}}');
    // Padding is normalised, so the value the parent stores is always canonical.
    expect(onChange).toHaveBeenCalledWith('Review {{ pr.title }}');
    // And the DOM is re-rendered from that model, so the braces become a pill.
    expect(editor.querySelectorAll('[data-slot="template-token"]')).toHaveLength(1);
    expect(editor.textContent).toBe('Review pr.title');
  });

  it('reads a pill back as its token rather than its label', () => {
    const onChange = vi.fn();
    const { container } = render(
      <TemplateField
        value="Review {{ pr.title }}"
        onChange={onChange}
        catalog={CATALOG}
        aria-label="Prompt"
      />,
    );
    const editor = editorOf(container);
    // Simulate typing one character after the pill.
    editor.appendChild(editor.ownerDocument.createTextNode('!'));
    fireEvent.input(editor);
    expect(onChange).toHaveBeenCalledWith('Review {{ pr.title }}!');
  });

  it('inserts a newline on Enter instead of letting the browser build markup', () => {
    const onChange = vi.fn();
    const { container } = render(
      <TemplateField value="a" onChange={onChange} catalog={CATALOG} aria-label="Prompt" />,
    );
    const editor = editorOf(container);
    const event = fireEvent.keyDown(editor, { key: 'Enter' });
    expect(event).toBe(false); // preventDefault was called
    expect(onChange).toHaveBeenCalledWith('a\n');
    expect(editor.querySelector('div')).toBeNull();
    expect(editor.querySelector('br')).toBeNull();
  });

  it('ignores an incoming value that disagrees with its own edit while focused', () => {
    const onChange = vi.fn();
    const { container, rerender } = render(
      <TemplateField value="" onChange={onChange} catalog={CATALOG} aria-label="Prompt" />,
    );
    const editor = editorOf(container);
    editor.focus();
    typeInto(editor, 'typed');
    // A parent that echoes something else back must not yank the DOM mid-edit.
    rerender(
      <TemplateField
        value="from parent"
        onChange={onChange}
        catalog={CATALOG}
        aria-label="Prompt"
      />,
    );
    expect(editor.textContent).toBe('typed');
  });

  it('re-renders from the value prop once focus has left', () => {
    const { container, rerender } = render(
      <TemplateField value="one" onChange={vi.fn()} catalog={CATALOG} aria-label="Prompt" />,
    );
    const editor = editorOf(container);
    rerender(
      <TemplateField
        value="two {{ pr.title }}"
        onChange={vi.fn()}
        catalog={CATALOG}
        aria-label="Prompt"
      />,
    );
    expect(editor.textContent).toBe('two pr.title');
  });

  it('shows the placeholder only while the template is empty', () => {
    const { container, rerender } = render(
      <TemplateField value="" onChange={vi.fn()} catalog={CATALOG} placeholder="Write a prompt…" />,
    );
    expect(screen.getByText('Write a prompt…')).toBeInTheDocument();
    rerender(
      <TemplateField
        value="x"
        onChange={vi.fn()}
        catalog={CATALOG}
        placeholder="Write a prompt…"
      />,
    );
    expect(container.querySelector('[data-slot="template-field-placeholder"]')).toBeNull();
  });

  it('honours minRows as a minimum editor height', () => {
    const { container } = render(
      <TemplateField value="" onChange={vi.fn()} catalog={CATALOG} minRows={6} />,
    );
    expect(editorOf(container).style.minHeight).toBe('142px');
  });

  it('disables editing and both toolbar controls when disabled', () => {
    const { container } = render(
      <TemplateField value="x" onChange={vi.fn()} catalog={CATALOG} disabled />,
    );
    const editor = editorOf(container);
    expect(editor).not.toHaveAttribute('contenteditable', 'true');
    expect(editor).toHaveAttribute('aria-disabled', 'true');
    expect(screen.getByRole('button', { name: 'Insert field' })).toBeDisabled();
  });

  it('does not emit while disabled', () => {
    const onChange = vi.fn();
    const { container } = render(
      <TemplateField value="x" onChange={onChange} catalog={CATALOG} disabled />,
    );
    fireEvent.input(editorOf(container));
    expect(onChange).not.toHaveBeenCalled();
  });
});

describe('TemplateField preview', () => {
  it('offers no preview when nothing in the catalog has an example', () => {
    render(
      <TemplateField
        value="{{ repo.full_name }}"
        onChange={vi.fn()}
        catalog={[{ token: 'repo.full_name', description: 'Owner and repository name' }]}
      />,
    );
    expect(screen.queryByRole('button', { name: /preview/i })).toBeNull();
  });

  it('substitutes examples, and renders fields without one as empty', () => {
    const { container } = render(
      <TemplateField
        value="Review {{ pr.title }} in {{ repo.full_name }}"
        onChange={vi.fn()}
        catalog={CATALOG}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: /preview/i }));
    const panel = container.querySelector('[data-slot="template-field-preview"]');
    // repo.full_name has no example, so it renders empty — exactly what the
    // backend does with a field it cannot resolve.
    expect(panel?.textContent).toContain('Review Fix login retry in ');
  });

  it('toggles the preview panel off again', () => {
    const { container } = render(
      <TemplateField value="{{ pr.title }}" onChange={vi.fn()} catalog={CATALOG} />,
    );
    const toggle = screen.getByRole('button', { name: /preview/i });
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-pressed', 'true');
    fireEvent.click(toggle);
    expect(container.querySelector('[data-slot="template-field-preview"]')).toBeNull();
  });
});

describe('TemplateField picker', () => {
  it('inserts the chosen field into the template as a canonical token', async () => {
    const onChange = vi.fn();
    const { container } = render(
      <TemplateField value="Review " onChange={onChange} catalog={CATALOG} aria-label="Prompt" />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Insert field' }));
    const item = await screen.findByText('pr.number');
    fireEvent.click(item);
    expect(onChange).toHaveBeenCalledWith('Review {{ pr.number }}');
    expect(editorOf(container).querySelectorAll('[data-slot="template-token"]')).toHaveLength(1);
  });
});

describe('TemplateCatalogList', () => {
  it('groups entries under their headings', () => {
    render(<TemplateCatalogList catalog={CATALOG} onSelect={vi.fn()} autoFocusSearch={false} />);
    expect(screen.getByText('Pull request')).toBeInTheDocument();
    expect(screen.getByText('Repo')).toBeInTheDocument();
    expect(screen.getByText('Group')).toBeInTheDocument();
  });

  it('inserts the bare token on click', () => {
    const onSelect = vi.fn();
    const { container } = render(
      <TemplateCatalogList catalog={CATALOG} onSelect={onSelect} autoFocusSearch={false} />,
    );
    fireEvent.click(container.querySelector('[data-token="pr.number"]')!);
    expect(onSelect).toHaveBeenCalledWith('pr.number');
  });

  it('lists a pattern family but never lets it be chosen', () => {
    const { container } = render(
      <TemplateCatalogList catalog={CATALOG} onSelect={vi.fn()} autoFocusSearch={false} />,
    );
    expect(screen.getByText('group.labels.<name>')).toBeInTheDocument();
    expect(container.querySelector('[data-token="group.labels.<name>"]')).toBeNull();
    expect(container.querySelector('[data-slot="template-catalog-pattern"]')).toHaveAttribute(
      'aria-disabled',
      'true',
    );
  });

  it('filters on token and description', () => {
    const { container } = render(
      <TemplateCatalogList catalog={CATALOG} onSelect={vi.fn()} autoFocusSearch={false} />,
    );
    fireEvent.change(screen.getByLabelText('Search fields'), { target: { value: 'owner' } });
    const items = container.querySelectorAll('[data-slot="template-catalog-item"]');
    expect(items).toHaveLength(1);
    expect(items[0]).toHaveAttribute('data-token', 'repo.full_name');
  });

  it('says so when nothing matches', () => {
    render(<TemplateCatalogList catalog={CATALOG} onSelect={vi.fn()} autoFocusSearch={false} />);
    fireEvent.change(screen.getByLabelText('Search fields'), { target: { value: 'zzz' } });
    expect(screen.getByText(/No fields match/)).toBeInTheDocument();
  });

  it('moves the active row with the arrow keys and inserts it on Enter', () => {
    const onSelect = vi.fn();
    const { container } = render(
      <TemplateCatalogList catalog={CATALOG} onSelect={onSelect} autoFocusSearch={false} />,
    );
    const search = screen.getByLabelText('Search fields');
    fireEvent.keyDown(search, { key: 'ArrowDown' });
    fireEvent.keyDown(search, { key: 'ArrowDown' });
    expect(container.querySelector('[data-active="true"]')).toHaveAttribute(
      'data-token',
      'repo.full_name',
    );
    fireEvent.keyDown(search, { key: 'Enter' });
    expect(onSelect).toHaveBeenCalledWith('repo.full_name');
  });

  it('wraps the active row past both ends, skipping pattern entries', () => {
    const { container } = render(
      <TemplateCatalogList catalog={CATALOG} onSelect={vi.fn()} autoFocusSearch={false} />,
    );
    const search = screen.getByLabelText('Search fields');
    fireEvent.keyDown(search, { key: 'ArrowUp' });
    expect(container.querySelector('[data-active="true"]')).toHaveAttribute(
      'data-token',
      'repo.full_name',
    );
    fireEvent.keyDown(search, { key: 'ArrowDown' });
    expect(container.querySelector('[data-active="true"]')).toHaveAttribute(
      'data-token',
      'pr.title',
    );
  });
});
