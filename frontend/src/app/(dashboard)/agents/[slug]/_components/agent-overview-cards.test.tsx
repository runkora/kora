import { describe, expect, it } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import { toAgentTypeRow } from '../../_types';
import { CODE_REVIEWER } from '../../_components/agents-mock-data';
import { GrantScopesCard } from './agent-overview-cards';

describe('GrantScopesCard', () => {
  it('says so when the agent has no grant scopes', () => {
    renderWithProviders(
      <GrantScopesCard agent={{ ...toAgentTypeRow(CODE_REVIEWER), grantScopes: [] }} />,
    );
    expect(screen.getByText('No grant scopes configured.')).toBeTruthy();
  });
});
