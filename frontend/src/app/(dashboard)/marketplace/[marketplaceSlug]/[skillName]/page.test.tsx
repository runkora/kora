import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { getSession, isSessionValid } from '@orbit/server/session';
import { loadSkillPage } from '../../_services/marketplace-server';
import SkillDetailPage from './page';

vi.mock('@orbit/server/session', () => ({ getSession: vi.fn(), isSessionValid: vi.fn() }));
vi.mock('../../_services/marketplace-server', () => ({ loadSkillPage: vi.fn() }));
vi.mock('next/navigation', () => ({ useRouter: () => ({ refresh: vi.fn() }) }));

const skill = {
  name: 'Reviewer',
  skillSlug: 'team__repo__reviewer',
  description: 'Reviews code.',
  skill: { name: 'SKILL.md', path: 'skills/reviewer/SKILL.md', content: '# Instructions' },
  documents: [],
};

async function renderPage(skillSegment = 'reviewer') {
  render(
    await SkillDetailPage({
      params: Promise.resolve({ marketplaceSlug: 'team__repo', skillName: skillSegment }),
    }),
  );
}

beforeEach(() => {
  vi.mocked(getSession).mockReset();
  vi.mocked(isSessionValid).mockReset().mockReturnValue(true);
  vi.mocked(loadSkillPage).mockReset().mockResolvedValue({
    ok: true,
    skill,
    marketplaceName: 'Team',
    marketplaceLink: 'https://github.com/team/repo',
  });
  // Only the access token is read after the session validator succeeds.
  vi.mocked(getSession).mockResolvedValue({ accessToken: 'token' } as Awaited<
    ReturnType<typeof getSession>
  >);
});

describe('Skill detail Server Component', () => {
  it('loads the skillName from the path and renders details with a clean return link', async () => {
    await renderPage();
    expect(loadSkillPage).toHaveBeenCalledExactlyOnceWith('token', 'team__repo', 'reviewer');
    expect(screen.getByRole('heading', { name: 'File Explorer' })).toBeVisible();
    expect(screen.getByText('# Instructions')).toBeVisible();
    expect(screen.getByRole('link', { name: 'Skills' })).toHaveAttribute('href', '/marketplace');
  });

  it('still loads an older link whose segment carries the full skillSlug', async () => {
    await renderPage(skill.skillSlug);
    expect(loadSkillPage).toHaveBeenCalledExactlyOnceWith('token', 'team__repo', skill.skillSlug);
  });

  it('rejects expired sessions before loading upstream data', async () => {
    vi.mocked(isSessionValid).mockReturnValue(false);
    await renderPage();
    expect(loadSkillPage).not.toHaveBeenCalled();
    expect(screen.getByRole('alert')).toHaveTextContent('Session expired');
  });

  it('keeps missing Skills distinct from failed loads', async () => {
    vi.mocked(loadSkillPage).mockResolvedValue({
      ok: false,
      status: 404,
      message: 'Skill not found',
    });
    await renderPage();
    expect(screen.getByText('Skill not found')).toBeVisible();
    expect(screen.getByRole('link', { name: 'Back to Skills' })).toHaveAttribute(
      'href',
      '/marketplace',
    );
    expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument();
  });

  it('offers a server refresh retry for upstream failures', async () => {
    vi.mocked(loadSkillPage).mockResolvedValue({ ok: false, status: 503, message: 'Unavailable' });
    await renderPage();
    expect(screen.getByRole('alert')).toHaveTextContent('could not load this Skill');
    expect(screen.getByRole('button', { name: 'Try again' })).toBeEnabled();
  });
});
