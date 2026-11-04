/** Unit tests for the skill page Server Component's three outcomes. */
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';
import { getSession, isSessionValid } from '@runkora/server/session';
import { loadSkill } from '../../_services/skills-server';
import { pdfSkill } from '../../_components/skills-story-fixtures';
import SkillPage from './page';

vi.mock('@runkora/server/session', () => ({ getSession: vi.fn(), isSessionValid: vi.fn() }));
vi.mock('../../_services/skills-server', () => ({
  loadSkill: vi.fn(),
  loadMarketplaces: vi.fn().mockResolvedValue({ ok: true, data: [] }),
  loadSkillSummary: vi.fn().mockResolvedValue(null),
}));
vi.mock('next/navigation', () => ({ useRouter: () => ({ refresh: vi.fn() }) }));

async function renderPage() {
  renderWithProviders(
    await SkillPage({
      params: Promise.resolve({ marketplace: 'anthropic-skills', path: ['skills', 'pdf'] }),
      searchParams: Promise.resolve({ marketplace: 'anthropic-skills', search: 'pdf', page: '2' }),
    }),
  );
}

beforeEach(() => {
  vi.mocked(isSessionValid).mockReturnValue(true);
  vi.mocked(getSession).mockResolvedValue({ accessToken: 'token' } as Awaited<
    ReturnType<typeof getSession>
  >);
});

describe('SkillPage', () => {
  it('loads the skill by its folder path and links back to the results it came from', async () => {
    vi.mocked(loadSkill).mockResolvedValue({ ok: true, data: pdfSkill() });

    await renderPage();

    expect(loadSkill).toHaveBeenCalledWith('token', 'anthropic-skills', 'skills/pdf');
    expect(screen.getByRole('heading', { name: 'PDF Processing Guide', level: 1 })).toBeVisible();
    expect(screen.getByRole('link', { name: /^Skills$/ })).toHaveAttribute(
      'href',
      '/skills?marketplace=anthropic-skills&search=pdf&page=2',
    );
  });

  it('shows a missing skill as not found', async () => {
    vi.mocked(loadSkill).mockResolvedValue({ ok: false, status: 404, message: 'raw' });

    await renderPage();

    expect(screen.getByText('Skill not found')).toBeVisible();
  });

  it('shows an unavailable Marketplace with its reason and a retry', async () => {
    vi.mocked(loadSkill).mockResolvedValue({ ok: false, status: 502, message: 'No credential.' });

    await renderPage();

    expect(screen.getByText('This Marketplace is unavailable.')).toBeVisible();
    expect(screen.getByText('No credential.')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeVisible();
  });
});
