/** @tier 3 — skills/[marketplace]/[...path]-scoped. The skill page in each of its states, loading files through the mocked BFF. */

import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import { dashboardPage } from '@runkora/storybook/dashboard-frame';
import type { MockRequest } from '@runkora/storybook/mock-api';
import {
  MARKETPLACE_FIXTURES,
  pdfFile,
  pdfSkill,
  PDF_SUMMARY,
} from '../../../_components/skills-story-fixtures';
import { SkillDetail } from './skill-detail';

const FAILING_FILE = 'scripts/fill_fillable_fields.py';

const meta: Meta<typeof SkillDetail> = {
  title: 'Pages/Skills/Detail',
  component: SkillDetail,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/skills/anthropic-skills/skills/pdf' } },
  },
  args: { skill: pdfSkill(), marketplace: MARKETPLACE_FIXTURES[0], summary: PDF_SUMMARY },
  decorators: [
    dashboardPage({
      'GET /api/skills/file': ({ url }: MockRequest) =>
        pdfFile(url.searchParams.get('file') ?? '', [FAILING_FILE]),
      'PUT /api/preferences': {},
    }),
  ],
};
export default meta;
type Story = StoryObj<typeof SkillDetail>;

/** Opens a file in the visible tree; the hidden copy of the rail is out of the accessibility tree. */
async function openFile(canvasElement: HTMLElement, folder: string | null, file: RegExp) {
  const canvas = within(canvasElement);
  if (folder)
    await userEvent.click(await canvas.findByRole('button', { name: `${folder} folder` }));
  await userEvent.click(await canvas.findByRole('button', { name: file }));
}

export const Default: Story = {};

export const OnlySkillMd: Story = {
  args: { skill: pdfSkill({ files: [] }) },
};

export const SelectedFile: Story = {
  play: async ({ canvasElement }) => {
    await openFile(canvasElement, 'scripts', /convert_pdf_to_images\.py/);
  },
};

export const SelectedMarkdownFile: Story = {
  play: async ({ canvasElement }) => {
    await openFile(canvasElement, null, /^forms\.md/);
  },
};

export const FileFailsToLoad: Story = {
  play: async ({ canvasElement }) => {
    await openFile(canvasElement, 'scripts', /fill_fillable_fields\.py/);
  },
};
