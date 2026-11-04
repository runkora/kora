/** Integration tests for the skill page: SKILL.md on open, other files on selection, the header actions. */
import { describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { renderWithProviders, server } from '@runkora/test-utils';
import {
  MARKETPLACE_FIXTURES,
  pdfSkill,
  PDF_SUMMARY,
} from '../../../_components/skills-story-fixtures';
import { SkillDetail } from './skill-detail';

vi.mock('next/navigation', () => ({ useRouter: () => ({ refresh: vi.fn() }) }));

function renderDetail() {
  renderWithProviders(
    <SkillDetail skill={pdfSkill()} marketplace={MARKETPLACE_FIXTURES[0]} summary={PDF_SUMMARY} />,
  );
}

async function openLicense() {
  renderDetail();
  await userEvent.click(screen.getAllByRole('button', { name: /LICENSE\.txt/ })[0]);
}

describe('SkillDetail', () => {
  it('renders SKILL.md without its frontmatter and title, and Raw shows the whole file', async () => {
    renderDetail();

    expect(screen.getByRole('heading', { name: 'PDF Processing Guide', level: 1 })).toBeVisible();
    expect(screen.queryByText(/^name: pdf/)).not.toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Overview' })).toBeVisible();

    await userEvent.click(screen.getByRole('button', { name: 'Raw' }));
    expect(screen.getByText(/^--- name: pdf/)).toBeVisible();
  });

  it('shows the version and chips from the list entry, and links View source to the folder at the commit', () => {
    renderDetail();

    expect(screen.getByText('pdf · v2.1.0')).toBeVisible();
    expect(screen.getByText('documents')).toBeVisible();
    expect(screen.getByRole('link', { name: /View source/ })).toHaveAttribute(
      'href',
      `https://github.com/anthropics/skills/tree/${pdfSkill().commit}/skills/pdf`,
    );
  });

  it('loads a picked file once, showing a loading state meanwhile', async () => {
    const requests: Record<string, string>[] = [];
    let release!: () => void;
    const held = new Promise<void>((resolve) => (release = resolve));
    server.use(
      http.get('/api/skills/file', async ({ request }) => {
        requests.push(Object.fromEntries(new URL(request.url).searchParams));
        await held;
        return HttpResponse.json({
          data: { path: 'LICENSE.txt', content: 'All rights reserved.' },
        });
      }),
    );

    await openLicense();
    expect(screen.getByRole('status', { name: 'Loading the file' })).toBeVisible();
    release();
    expect(await screen.findByText('All rights reserved.')).toBeVisible();

    await userEvent.click(screen.getByRole('button', { name: 'SKILL.md' }));
    await userEvent.click(screen.getAllByRole('button', { name: /LICENSE\.txt/ })[0]);
    expect(requests).toEqual([
      { marketplace: 'anthropic-skills', path: 'skills/pdf', file: 'LICENSE.txt' },
    ]);
  });

  it('shows an unavailable file with the API message and retries it', async () => {
    let fail = true;
    server.use(
      http.get('/api/skills/file', () =>
        fail
          ? HttpResponse.json(
              { message: 'This Marketplace could not be loaded. Try again later.' },
              { status: 502 },
            )
          : HttpResponse.json({ data: { path: 'LICENSE.txt', content: 'All rights reserved.' } }),
      ),
    );

    await openLicense();
    expect(await screen.findByText('This Marketplace is unavailable.')).toBeVisible();

    fail = false;
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByText('All rights reserved.')).toBeVisible();
  });

  it('adds the skill to the favourites', async () => {
    server.use(http.put('/api/preferences', () => HttpResponse.json({})));
    renderDetail();

    await userEvent.click(screen.getByRole('button', { name: 'Add to favourites' }));

    expect(screen.getByRole('button', { name: 'Remove from favourites' })).toBeVisible();
  });
});
