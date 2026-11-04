/** @tier 3 — skills-scoped. One small builder of skills-service sample data, shared by the Skills stories and tests. */

import type {
  Marketplace,
  Skill,
  SkillFileEntry,
  SkillSummary,
  SkillsPage,
} from '@runkora/shared/api';
import { PAGE_SIZE, skillKey, type SkillsBrowse, type SkillsOverview } from '../_types';

const COMMIT = '3f2a9c1e0b7d4f5a6c8e9d0f1a2b3c4d5e6f7a8b';
const TWO_HOURS_AGO = new Date(Date.now() - 2 * 60 * 60 * 1000).toISOString();

type Extras = Partial<Pick<SkillSummary, 'version' | 'tags' | 'tools' | 'manualOnly'>>;

/** One list entry. The path defaults to `skills/<name>` and the title to the name. */
function skill(
  marketplace: string,
  name: string,
  title: string,
  description: string,
  extras: Extras & { path?: string } = {},
): SkillSummary {
  const { path = `skills/${name}`, ...rest } = extras;
  return {
    marketplace,
    path,
    name,
    title,
    description,
    version: null,
    tags: [],
    tools: [],
    manualOnly: false,
    ...rest,
  };
}

function marketplace(slug: string, repository: string, skills: SkillSummary[]): Marketplace {
  return {
    slug,
    name: repository.split('/')[1] ?? repository,
    repository,
    ref: 'main',
    commit: COMMIT,
    syncedAt: TWO_HOURS_AGO,
    skillCount: skills.length,
  };
}

const PDF = skill(
  'anthropic-skills',
  'pdf',
  'PDF Processing Guide',
  'Comprehensive PDF manipulation toolkit for extracting text and tables, creating new PDFs, merging and splitting documents, and handling forms. Use when a PDF form needs filling or PDFs need processing at scale.',
  {
    version: '2.1.0',
    tags: ['documents', 'forms', 'extraction'],
    tools: ['Bash', 'Read', 'Write'],
  },
);

const A = (name: string, title: string, description: string, extras?: Extras) =>
  skill('anthropic-skills', name, title, description, extras);
const ANTHROPIC_SKILLS = [
  A(
    'academy-guide',
    'Claude Academy guide',
    'Answers questions about Anthropic Academy courses and points learners to the lesson that covers their question.',
  ),
  A(
    'algorithmic-art',
    'Algorithmic art',
    'Creates algorithmic art with p5.js using seeded randomness and interactive parameter exploration. Use for generative art, flow fields or particle systems.',
  ),
  A(
    'brand-guidelines',
    'Anthropic Brand Styling',
    "Applies Anthropic's official brand colours and typography to any artifact that should have the Anthropic look and feel.",
  ),
  A(
    'canvas-design',
    'Canvas design',
    'Creates visual art in PNG and PDF documents from a design philosophy. Use for a poster, a piece of art or another static design.',
  ),
  A(
    'claude-api',
    'Building LLM-Powered Applications with Claude',
    'Builds applications on the Claude API and the Anthropic SDKs: messages, tool use, streaming, prompt caching and batch requests.',
    { version: '0.9.2', tags: ['api', 'sdk'] },
  ),
  A(
    'docx',
    'DOCX creation, editing, and analysis',
    'Creates, edits and analyses Word documents with support for tracked changes, comments, formatting preservation and text extraction.',
  ),
  A(
    'mcp-builder',
    'MCP Server Development Guide',
    'Guides the creation of high-quality MCP servers that let LLMs reach external services through well-designed tools, in Python or TypeScript.',
    { version: '1.4.0', tags: ['mcp', 'typescript', 'python'] },
  ),
  PDF,
  A(
    'pptx',
    'PPTX creation, editing, and analysis',
    'Creates, edits and analyses presentations: new decks, layouts, speaker notes and comments in .pptx files.',
  ),
  A(
    'skill-creator',
    'Skill creator',
    "Guides the creation of effective skills that extend Claude's capabilities with specialised knowledge, workflows or tool integrations.",
    { manualOnly: true },
  ),
  A(
    'theme-factory',
    'Theme Factory Skill',
    'Styles artifacts with one of ten preset themes of colours and fonts, for slides, documents, reports and landing pages.',
  ),
  A(
    'webapp-testing',
    'Web Application Testing',
    'Tests local web applications with Playwright: verifies frontend behaviour, captures screenshots and reads browser logs.',
    { tags: ['testing', 'playwright'], tools: ['Bash', 'Read'] },
  ),
];

const O = (name: string, title: string, description: string, extras?: Extras) =>
  skill('partner-skills', name, title, description, extras);
const SUPERPOWERS_SKILLS = [
  O(
    'brainstorming',
    'Brainstorming ideas into designs',
    'Refines a rough idea into a design through questions, alternatives and incremental validation before any code is written.',
  ),
  O(
    'condition-based-waiting',
    'Condition-based waiting',
    'Replaces arbitrary timeouts in tests with waits on the condition the test actually depends on.',
  ),
  O(
    'systematic-debugging',
    'Systematic debugging',
    'Finds the root cause of a bug in four phases before proposing any fix.',
    { tags: ['debugging', 'testing'] },
  ),
  O(
    'test-driven-development',
    'Test-driven development',
    'Writes the failing test first, watches it fail, then writes the least code that passes.',
  ),
];

const M = (name: string, title: string, description: string) =>
  skill('mattpocock-skills', name, title, description, { path: `skills/engineering/${name}` });
const MATTPOCOCK_SKILLS = [
  M(
    'code-review',
    'Code review',
    'Review a branch or PR against its spec and the codebase conventions.',
  ),
  M(
    'prototype',
    'Prototype',
    'Build a throwaway prototype to answer one design question before committing to it.',
  ),
  M('tdd', 'TDD', 'Test-driven development with red, green and refactor, one behaviour at a time.'),
  M(
    'triage',
    'Triage',
    'Triage incoming issues into bugs, features and questions with a next step for each.',
  ),
];

/** The three sample Marketplaces, with their skills in title order as skills-service returns them. */
const SKILL_FIXTURES = [...ANTHROPIC_SKILLS, ...SUPERPOWERS_SKILLS, ...MATTPOCOCK_SKILLS].sort(
  (a, b) => a.title.localeCompare(b.title, 'en', { sensitivity: 'base' }),
);
export const MARKETPLACE_FIXTURES = [
  marketplace('anthropic-skills', 'anthropics/skills', ANTHROPIC_SKILLS),
  marketplace('partner-skills', 'obra/superpowers', SUPERPOWERS_SKILLS),
  marketplace('mattpocock-skills', 'mattpocock/skills', MATTPOCOCK_SKILLS),
];

/** What skills-service answers for one overview page; the stories' BFF mock and tests use it. */
export function skillsPage(browse: SkillsBrowse, keys: string[] = []): SkillsPage {
  const query = browse.search.toLowerCase();
  const matched = SKILL_FIXTURES.filter(
    (s) =>
      (!browse.favourites || keys.includes(skillKey(s))) &&
      (!browse.marketplace || s.marketplace === browse.marketplace) &&
      (!query || `${s.title} ${s.name} ${s.description}`.toLowerCase().includes(query)),
  );
  const offset = (browse.page - 1) * PAGE_SIZE;
  return {
    skills: matched.slice(offset, offset + PAGE_SIZE),
    total: matched.length,
    offset,
    limit: PAGE_SIZE,
  };
}

export function storyOverview(
  browse: Partial<SkillsBrowse> = {},
  keys: string[] = [],
): SkillsOverview {
  const full = { marketplace: '', favourites: false, search: '', page: 1, ...browse };
  const page = skillsPage(full, keys);
  return {
    marketplaces: MARKETPLACE_FIXTURES,
    browse: full,
    skills: page.skills,
    total: page.total,
    failures: [],
  };
}

/** A few saved favourites, as the preferences document would hold them. */
export const STORY_FAVOURITES = [
  'anthropic-skills:skills/pptx',
  'anthropic-skills:skills/webapp-testing',
  'mattpocock-skills:skills/engineering/prototype',
  'partner-skills:skills/systematic-debugging',
];

// ── The pdf skill, which the detail stories and tests open ──────────────────

const kb = (value: number) => Math.round(value * 1024);

const PDF_FILES: SkillFileEntry[] = [
  { path: 'LICENSE.txt', size: kb(1.5) },
  { path: 'forms.md', size: kb(11.6) },
  { path: 'reference.md', size: kb(16.3) },
  { path: 'scripts/check_fillable_fields.py', size: 342 },
  { path: 'scripts/convert_pdf_to_images.py', size: kb(1.0) },
  { path: 'scripts/fill_fillable_fields.py', size: kb(4.0) },
];

const PDF_SKILL_MD = `---
name: pdf
description: ${PDF.description}
license: Proprietary. LICENSE.txt has complete terms
---

# PDF Processing Guide

## Overview

This guide covers essential PDF processing operations using Python libraries and command-line tools. For advanced features, JavaScript libraries and detailed examples, see \`reference.md\`. If you need to fill out a PDF form, read \`forms.md\` and follow its instructions.

## Quick start

\`\`\`python
from pypdf import PdfReader

reader = PdfReader("document.pdf")
print(f"Pages: {len(reader.pages)}")
text = "".join(page.extract_text() for page in reader.pages)
\`\`\`

## Python libraries

### pypdf: basic operations

Merge, split and rotate pages without touching their content.

\`\`\`python
from pypdf import PdfReader, PdfWriter

writer = PdfWriter()
for path in ["a.pdf", "b.pdf"]:
    for page in PdfReader(path).pages:
        writer.add_page(page)
writer.write("merged.pdf")
\`\`\`

### pdfplumber: text and tables

Use \`pdfplumber\` when the layout matters, for example to keep table columns apart.

1. Open the file with \`pdfplumber.open\`.
2. Call \`page.extract_tables()\` on each page.
3. Load each table into a DataFrame and concatenate them.

## Command-line tools

- \`pdftotext -layout input.pdf output.txt\` keeps the page layout.
- \`qpdf --split-pages input.pdf out-%d.pdf\` writes one file per page.
- \`qpdf --password=secret --decrypt in.pdf out.pdf\` removes a known password.

## Forms

Run \`scripts/check_fillable_fields.py\` first. A form with fillable fields goes through \`scripts/fill_fillable_fields.py\`; a form without them needs annotations, as \`forms.md\` describes.
`;

const PDF_FILE_BODIES: Record<string, string> = {
  'LICENSE.txt':
    '© 2025 Anthropic, PBC. All rights reserved.\n\nLICENSE: Use of these materials is governed by your agreement with Anthropic regarding use of its services.',
  'forms.md': `# Filling PDF forms

**CRITICAL: complete these steps in order.**

## Fillable fields

1. Run \`python scripts/extract_form_field_info.py input.pdf fields.json\`.
2. Write a \`values.json\` that maps each field id to its value.
3. Run \`python scripts/fill_fillable_fields.py input.pdf values.json output.pdf\`.

## Forms without fillable fields

Convert each page to an image with \`scripts/convert_pdf_to_images.py\`, find the label of each field, and record a bounding box for the text that belongs next to it. Then check the boxes with \`scripts/check_bounding_boxes.py\` before writing the annotations.
`,
  'reference.md': `# PDF processing reference

## pypdfium2

Fast rendering of pages to images, backed by PDFium.

\`\`\`python
import pypdfium2 as pdfium

pdf = pdfium.PdfDocument("document.pdf")
image = pdf[0].render(scale=2).to_pil()
image.save("page-1.png")
\`\`\`

## JavaScript: pdf-lib

Create and modify PDFs in any JavaScript environment.
`,
  'scripts/check_fillable_fields.py': `import sys
from pypdf import PdfReader

# Tells Claude whether the form has fillable fields, which decides the fill path.
reader = PdfReader(sys.argv[1])
if reader.get_fields():
    print("This PDF has fillable form fields")
else:
    print("This PDF does not have fillable form fields; see forms.md")
`,
  'scripts/convert_pdf_to_images.py': `import os
import sys
from pdf2image import convert_from_path


def convert(pdf_path, output_dir, max_dim=1000):
    images = convert_from_path(pdf_path, dpi=200)
    for i, image in enumerate(images):
        width, height = image.size
        if width > max_dim or height > max_dim:
            scale = min(max_dim / width, max_dim / height)
            image = image.resize((int(width * scale), int(height * scale)))
        image.save(os.path.join(output_dir, f"page_{i + 1}.png"))
    print(f"Converted {len(images)} pages to PNG images")


if __name__ == "__main__":
    convert(sys.argv[1], sys.argv[2])
`,
};

/** The pdf detail as skills-service returns it. */
export function pdfSkill(overrides: Partial<Skill> = {}): Skill {
  return {
    marketplace: PDF.marketplace,
    path: PDF.path,
    name: PDF.name,
    title: PDF.title,
    description: PDF.description,
    repository: 'anthropics/skills',
    ref: 'main',
    commit: COMMIT,
    version: PDF.version,
    tags: PDF.tags,
    tools: PDF.tools,
    manualOnly: PDF.manualOnly,
    content: PDF_SKILL_MD,
    files: PDF_FILES,
    ...overrides,
  };
}

/** One file body, or a 502 for the paths in `failing`, as GET /api/skills/file answers. */
export function pdfFile(path: string, failing: string[] = []) {
  if (failing.includes(path)) {
    return new Response(
      JSON.stringify({ message: 'This Marketplace could not be loaded. Try again later.' }),
      { status: 502, headers: { 'Content-Type': 'application/json' } },
    );
  }
  return { data: { path, content: PDF_FILE_BODIES[path] ?? `Contents of ${path}.\n` } };
}
