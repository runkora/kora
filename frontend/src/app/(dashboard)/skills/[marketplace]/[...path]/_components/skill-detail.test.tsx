/** Integration-style unit tests for the skill files: SKILL.md on open, other files on selection. */
import { describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { renderWithProviders, server } from '@runkora/test-utils';
import { STORY_SKILL } from '../../../_components/skills-story-fixtures';
import { SkillDetail } from './skill-detail';

vi.mock('next/navigation', () => ({ useRouter: () => ({ refresh: vi.fn() }) }));

async function openForms() {
  renderWithProviders(<SkillDetail skill={STORY_SKILL} marketplace="team-skills" />);
  expect(screen.getByRole('figure')).toHaveTextContent('Read the PDF');
  await userEvent.click(screen.getByRole('button', { name: /reference/ }));
  await userEvent.click(screen.getByRole('button', { name: /forms\.md/ }));
}

describe('SkillDetail', () => {
  it('shows SKILL.md on open and loads a picked file once, showing a loading state meanwhile', async () => {
    const requests: URLSearchParams[] = [];
    let release!: () => void;
    const held = new Promise<void>((resolve) => (release = resolve));
    server.use(
      http.get('/api/skills/file', async ({ request }) => {
        requests.push(new URL(request.url).searchParams);
        await held;
        return HttpResponse.json({
          data: { path: 'reference/forms.md', content: 'Fill each field.' },
        });
      }),
    );

    await openForms();
    expect(screen.getByRole('status', { name: 'Loading the file' })).toBeVisible();
    release();
    expect(await screen.findByText('Fill each field.')).toBeVisible();

    await userEvent.click(screen.getByRole('button', { name: /SKILL\.md/ }));
    await userEvent.click(screen.getByRole('button', { name: /forms\.md/ }));
    expect(screen.getByText('Fill each field.')).toBeVisible();
    expect(requests).toHaveLength(1);
    expect(Object.fromEntries(requests[0])).toEqual({
      marketplace: 'team-skills',
      path: 'skills/pdf',
      file: 'reference/forms.md',
    });
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
          : HttpResponse.json({
              data: { path: 'reference/forms.md', content: 'Fill each field.' },
            }),
      ),
    );

    await openForms();
    expect(await screen.findByText('This Marketplace is unavailable.')).toBeVisible();
    expect(
      screen.getByText('This Marketplace could not be loaded. Try again later.'),
    ).toBeVisible();

    fail = false;
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByText('Fill each field.')).toBeVisible();
  });
});
