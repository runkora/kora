/** @tier 3 — marketplace-scoped. Preview of a Skill's contents and source details. */

import type { Meta, StoryObj } from '@storybook/react';
import type { Skill } from '@orbit/shared/api';
import { MarketplaceSkillDetail } from './marketplace-skill-detail';

const sampleSkill: Skill = {
  skillSlug: 'platform-engineering__skills__react-expert',
  name: 'React Expert',
  description: 'Reviews React components for accessibility, state handling, and clear interfaces.',
  skill: {
    path: 'skills/react-expert/SKILL.md',
    name: 'SKILL.md',
    content: `# React Expert

Review the existing components and explain the changes needed.

## Review checklist
- Check keyboard navigation and accessible names.
- Keep state close to the components that use it.
- Describe how loading, empty, and error states appear.`,
  },
  documents: [
    {
      path: 'skills/react-expert/references/accessibility.md',
      name: 'accessibility.md',
      content: `# Accessibility checks

- Interactive controls have clear accessible names.
- Focus remains visible during keyboard navigation.
- Loading and error messages communicate the current state.`,
    },
    {
      path: 'skills/react-expert/references/state-patterns.md',
      name: 'state-patterns.md',
      content: `# State patterns

Keep local UI state in the component that owns it.
Share state through a provider when several descendants need the same values and actions.`,
    },
  ],
};

const longTextSkill: Skill = {
  skillSlug:
    'platform-engineering__skills__production-react-accessibility-and-state-coordination-review',
  name: 'Production React Accessibility and State Coordination Review for Complex Dashboard Workflows',
  description:
    'Reviews complex React dashboards with search, pagination, source selection, and several result layouts. Explains how keyboard focus, accessible names, request cancellation, and retained results should behave when people switch between views or recover from a failed request.',
  skill: {
    path: 'skills/production-react-accessibility-and-state-coordination-review/guides/dashboard-workflows/implementation-and-review/SKILL.md',
    name: 'SKILL.md',
    content: `# Production React Accessibility and State Coordination Review

Review the existing dashboard before proposing changes. Trace the initial server response, the client provider, and each control that starts another request. Explain which component owns each piece of state and how the rendered results stay consistent with the selected source, search term, page, page size, and sort order.

## Keyboard and accessibility review
Follow the complete workflow using only the keyboard: enter a search term, choose another source, change the sort order, move to the next page, switch the result layout, and open a Skill. Check that every interactive control has a clear accessible name, that focus remains visible, and that a loading or error notice does not unexpectedly remove the control the person is using.

## Request coordination and recovery
When a later request fails, keep the previous successful results visible and explain that they may no longer match the requested filters. Show a retry action next to the error message. A slower response from an earlier request must not replace the results of a newer search, and cancelling a request must not be presented as a failure that the person needs to resolve.

## Review report
- Describe the observed behavior with a concrete sequence of actions and the visible result after each action.
- Identify the component or hook responsible for each proposed change, and preserve the existing responsibilities of the server loader, client service, provider, and presentation components.
- Review the same content on a wide screen and a narrow screen, including long descriptions, source paths, instruction paragraphs, and supporting resources.

Finish with the checks you actually ran and any behavior that still needs investigation. The report should give another engineer enough context to reproduce the observations without needing to read the entire implementation first.`,
  },
  documents: [
    {
      path: 'skills/production-react-accessibility-and-state-coordination-review/references/accessibility.md',
      name: 'accessibility.md',
      content: `# Accessibility review notes

Accessible names should describe the action rather than the icon. A source picker should identify the selected Marketplace, result rows and cards should identify the Skill they open, and pagination controls should communicate their destination and disabled boundaries. Instructions must remain readable when the available width changes.

Check focus before and after every request. Loading notices, retained results, and retry actions should remain understandable without relying on color, animation, or a pointer. Record any place where keyboard navigation loses its position or reaches a control whose purpose is unclear.`,
    },
    {
      path: 'skills/production-react-accessibility-and-state-coordination-review/references/state-patterns.md',
      name: 'state-patterns.md',
      content: `# State coordination notes

Keep local presentation choices close to their owner, and share browse data through the provider when several descendants need the same values and actions. A change to the selected Marketplace, search term, sort order, or page size should establish the appropriate first result page rather than retaining a page that no longer exists.

Distinguish the requested browse values from the last successful results. This makes it possible to show a pending request or a recoverable error while keeping useful content on screen, and gives the retry action enough information to repeat the operation the person intended.`,
    },
  ],
};

const meta: Meta<typeof MarketplaceSkillDetail> = {
  title: 'Pages/Marketplace/SkillDetail',
  component: MarketplaceSkillDetail,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof MarketplaceSkillDetail>;

export const Default: Story = {
  args: { skill: sampleSkill, marketplaceName: 'Platform Engineering' },
};

export const WithoutResources: Story = {
  args: {
    skill: { ...sampleSkill, documents: [] },
    marketplaceName: 'Platform Engineering',
  },
};

export const LongText: Story = {
  args: {
    skill: longTextSkill,
    marketplaceName: 'Platform Engineering and Application Reliability Reference Library',
  },
};
